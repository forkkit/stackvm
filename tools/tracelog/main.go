package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type machID [3]int

var zeroMachID machID

type record struct {
	kind     recordKind
	mid, cid machID
	count    int
	ip       uint64
	act      string
	rest     string
	notes    []string
}

type recordKind int

const (
	unknownLine = recordKind(iota)
	genericLine
	copyLine
	beginLine
	endLine
	hndlLine
	noteLine
	preOpLine
	postOpLine
)

func (rk recordKind) String() string {
	switch rk {
	case unknownLine:
		return "unknown"
	case genericLine:
		return "generic"
	case copyLine:
		return "copy"
	case beginLine:
		return "begin"
	case endLine:
		return "end"
	case hndlLine:
		return "hndl"
	case noteLine:
		return "note"
	case preOpLine:
		return "preOp"
	case postOpLine:
		return "postOp"
	default:
		return ""
	}
}

func (ss sessions) parseAll(r io.Reader) error {
	var cur machID
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Bytes()
		mid, rest := parseMidLog(line)
		if rest == nil {
			ss.extend(cur, strings.TrimRight(string(line), " \r\n"))
			continue
		}

		rec := parseRecord(mid, rest)
		sess := ss.session(rec.mid)
		rec = sess.add(rec)
		if rec.kind == copyLine {
			ss.session(rec.cid).addCoCopyRec(rec)
		}

		switch rec.kind {
		case unknownLine:
			ss.extend(cur, strings.TrimRight(string(line), " \r\n"))
		case hndlLine:
			cur = zeroMachID
		default:
			cur = rec.mid
		}
	}
	return sc.Err()
}

var midLogPat = regexp.MustCompile(`(?:\w+\.go:\d+: +|^)(\d+)\((\d+):(\d+)\) +(.+)`)

func parseMidLog(line []byte) (mid machID, rest []byte) {
	if match := midLogPat.FindSubmatch(line); match != nil {
		mid[0], _ = strconv.Atoi(string(match[1]))
		mid[1], _ = strconv.Atoi(string(match[2]))
		mid[2], _ = strconv.Atoi(string(match[3]))
		rest = match[4]
	}
	return
}

var recPat = regexp.MustCompile(`# +(\d+) +(.+) +@0x([0-9a-z]+)(?: +(.+))?`)

func parseRecord(mid machID, rest []byte) (rec record) {
	rec.mid = mid

	match := recPat.FindSubmatch(rest)
	if match == nil {
		rec.kind = noteLine
		rec.rest = string(rest)
		return
	}

	rec.count, _ = strconv.Atoi(string(match[1]))
	rec.act = strings.TrimRight(string(match[2]), " \r\n")
	rec.ip, _ = strconv.ParseUint(string(match[3]), 16, 64)
	rec.rest = string(match[4])

	return
}

var (
	markPat = regexp.MustCompile(`^(\+\+\+|===|\.\.\.|>>>)\s*`)
	midPat  = regexp.MustCompile(`(\d+)\((\d+):(\d+)\)`)
)

func scanChildKV(s string) (cid machID, r string) {
	var parts []string
	scanKVs(s, func(k, v string) {
		switch k {
		case "child":
			if match := midPat.FindStringSubmatch(v); match != nil {
				cid[0], _ = strconv.Atoi(match[1])
				cid[1], _ = strconv.Atoi(match[2])
				cid[2], _ = strconv.Atoi(match[3])
				parts = append(parts, fmt.Sprintf("child=%v", cid))
			}
		default:
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
	})
	r = strings.Join(parts, " ")
	return
}

func (sess *session) handleEndKV(k, v string) {
	switch k {
	case "err":
		sess.err = v
	default:
		sess.extra[k] = v
	}
}

func (sess *session) add(rec record) record {
	if rec.kind == noteLine {
		if m := markPat.FindStringSubmatchIndex(rec.rest); m != nil {
			rec.rest = rec.rest[m[1]:]
		}
		if i := len(sess.recs) - 1; i >= 0 {
			sess.recs[i].notes = append(sess.recs[i].notes, rec.rest)
		} else {
			sess.recs = append(sess.recs, rec)
		}
		return rec
	}

	if m := markPat.FindStringSubmatchIndex(rec.act); m != nil {
		mark := rec.act[m[2]:m[3]]
		rec.act = rec.act[m[1]:]
		switch mark {
		case ">>>":
			rec.kind = preOpLine
		case "...":
			rec.kind = postOpLine
		case "+++":
			rec.kind = copyLine
			rec.cid, rec.rest = scanChildKV(rec.rest)
		case "===":
			switch rec.act {
			case "Begin":
				rec.kind = beginLine
			case "End":
				rec.kind = endLine
				scanKVs(rec.rest, sess.handleEndKV)
			case "Handle":
				rec.kind = hndlLine
			}
		default:
			rec.kind = genericLine
		}
	}
	rec.act = strings.TrimSpace(rec.act)
	sess.recs = append(sess.recs, rec)
	return rec
}

func (sess *session) addCoCopyRec(rec record) {
	sess.pid = rec.mid
	var parts []string
	scanKVs(rec.rest, func(k, v string) {
		switch k {
		case "child":
			parts = append(parts, fmt.Sprintf("parent=%v", rec.mid))
		default:
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
	})
	rec.mid, rec.cid = rec.cid, zeroMachID
	rec.rest = strings.Join(parts, " ")
	sess.recs = append(sess.recs, rec)
}

type sessions map[machID]*session

type session struct {
	mid, pid machID
	recs     []record
	err      string
	extra    map[string]string
	unknown  []string
}

func (mid machID) String() string {
	return fmt.Sprintf("%d(%d:%d)", mid[0], mid[1], mid[2])
}

func (rec record) String() string {
	if rec.kind == noteLine {
		return fmt.Sprintf("% 10v % 13s %s", rec.mid, "", rec.rest)
	}
	return fmt.Sprintf("% 10v #% 4d @%#04x % -30s %s", rec.mid, rec.count, rec.ip, rec.act, rec.rest)
}

func (ss sessions) session(mid machID) *session {
	sess := ss[mid]
	if sess == nil {
		sess = &session{
			mid:   mid,
			extra: make(map[string]string),
		}
		ss[mid] = sess
	}
	return sess
}

func (ss sessions) extend(mid machID, s string) {
	sess := ss.session(mid)
	sess.unknown = append(sess.unknown, s)
}

func (ss sessions) idPath(sess *session) []machID {
	n := 1
	for id := sess.pid; id != zeroMachID; id = ss[id].pid {
		n++
	}
	ids := make([]machID, n)
	i := len(ids) - 1
	ids[i] = sess.mid
	for id := sess.pid; id != zeroMachID; id = ss[id].pid {
		i--
		ids[i] = id
	}
	return ids
}

func (ss sessions) fullID(sess *session) string {
	var buf bytes.Buffer
	ids := ss.idPath(sess)
	buf.WriteString(strconv.Itoa(sess.mid[0]))
	buf.WriteRune('(')
	buf.WriteString(strconv.Itoa(ids[0][2]))
	for i := 1; i < len(ids); i++ {
		buf.WriteRune(':')
		buf.WriteString(strconv.Itoa(ids[i][2]))
	}
	buf.WriteRune(')')
	return buf.String()
}

func (ss sessions) sessionLog(sess *session, logf func(string, ...interface{}) error) error {
	ids := ss.idPath(sess)
	for i, j := 0, 1; j < len(ids); i, j = i+1, j+1 {
		sess := ss[ids[i]]
		for _, rec := range sess.recs {
			if rec.kind == copyLine && rec.cid == ids[j] {
				break
			}
			if err := logf("%v", rec); err != nil {
				return err
			}
			for _, note := range rec.notes {
				logf("    %s", note)
			}
		}
	}
	for _, rec := range sess.recs {
		if err := logf("%v", rec); err != nil {
			return err
		}
		for _, note := range rec.notes {
			logf("    %s", note)
		}
	}
	for _, line := range sess.extra {
		if err := logf("%s", line); err != nil {
			return err
		}
	}
	return nil
}

func parseSessions(r io.Reader) (sessions, error) {
	ss := make(sessions)
	return ss, ss.parseAll(r)
}

type intsetFlag map[int]struct{}

func (ns intsetFlag) String() string   { return fmt.Sprint(map[int]struct{}(ns)) }
func (ns intsetFlag) Get() interface{} { return map[int]struct{}(ns) }
func (ns intsetFlag) Set(s string) error {
	for _, ss := range strings.Split(s, ",") {
		n, err := strconv.Atoi(ss)
		if err != nil {
			return err
		}
		ns[n] = struct{}{}
	}
	return nil
}

var haltPat = regexp.MustCompile(`HALT\((\d+)\)`)

func printSession(sessions sessions, mid machID) (err error) {
	sess := sessions[mid]
	if sess.err != "" {
		_, err = fmt.Printf("%s\terr=%v\n", sessions.fullID(sess), sess.err)
	} else {
		_, err = fmt.Printf("%s\textra=%v\n", sessions.fullID(sess), sess.extra)
	}
	return
}

func printFullSession(sessions sessions, mid machID) (err error) {
	err = printSession(sessions, mid)
	if err == nil {
		err = sessions.sessionLog(sessions[mid], indentPrintf("	").Printf)
	}
	if err == nil {
		_, err = fmt.Println()
	}
	return
}

type indentPrintf string

func (s indentPrintf) Printf(format string, args ...interface{}) error {
	_, err := fmt.Printf(string(s)+format+"\n", args...)
	return err
}

type recDat struct {
	Kind   string                 `json:"kind"`
	Action string                 `json:"action"`
	Count  int                    `json:"count"`
	IP     int                    `json:"ip"`
	Extra  map[string]interface{} `json:"extra"`
	Notes  []string               `json:"notes"`
}

type sessDat struct {
	ID       string            `json:"id"`
	ParentID *string           `json:"parent_id"`
	Error    string            `json:"error"`
	Records  []recDat          `json:"records"`
	Extra    map[string]string `json:"extra"`
	Unknown  []string          `json:"unknown"`
}

func (sess *session) toJSON() sessDat {
	dat := sessDat{
		ID:      sess.mid.String(),
		Error:   sess.err,
		Records: make([]recDat, len(sess.recs)),
		Extra:   sess.extra,
		Unknown: sess.unknown,
	}

	if sess.pid != zeroMachID {
		pidStr := sess.pid.String()
		dat.ParentID = &pidStr
	}

	for i, rec := range sess.recs {
		rd := recDat{
			Kind:   rec.kind.String(),
			Action: rec.act,
			Count:  rec.count,
			IP:     int(rec.ip),
			Extra:  make(map[string]interface{}),
			Notes:  rec.notes,
		}
		if rec.cid != zeroMachID {
			rd.Extra["child"] = rec.cid.String()
		}
		scanKVs(rec.rest, func(k, v string) {
			rd.Extra[k] = parseValue(v)
		})
		dat.Records[i] = rd
	}

	return dat
}

type jsonDumper struct {
	wc  io.WriteCloser
	st  bool
	enc *json.Encoder
}

func newJSONDumper(wc io.WriteCloser) sessionWriter {
	return &jsonDumper{
		wc:  wc,
		enc: json.NewEncoder(wc),
	}
}

func (jd *jsonDumper) WriteSession(sessions sessions, mid machID) error {
	if mid == zeroMachID {
		return nil
	}
	var err error
	if !jd.st {
		_, err = jd.wc.Write([]byte("[\n  "))
		if err == nil {
			jd.st = true
		}
	} else {
		_, err = jd.wc.Write([]byte(", "))
	}
	if err != nil {
		return err
	}
	return jd.enc.Encode(sessions[mid].toJSON())
}

func (jd *jsonDumper) Close() (err error) {
	if jd.st {
		_, err = jd.wc.Write([]byte("]\n"))
	}
	if cerr := jd.wc.Close(); err == nil {
		err = cerr
	}
	return err
}

type htmlDumper struct {
	sessionWriter
	wc   io.WriteCloser
	tmpl *template.Template
	buf  bytes.Buffer
}

type nopWriteCloser struct{ io.Writer }

func (nwf nopWriteCloser) Close() error { return nil }

func newHTMLDumper(name string, wc io.WriteCloser) (sessionWriter, error) {
	tmpl, err := parseTemplateAsset(name)
	if err != nil {
		return nil, err
	}
	hd := htmlDumper{
		wc:   wc,
		tmpl: tmpl,
	}
	hd.sessionWriter = newJSONDumper(nopWriteCloser{&hd.buf})
	return &hd, nil
}

func (hd *htmlDumper) Close() (err error) {
	err = hd.sessionWriter.Close()
	if err == nil {
		err = hd.tmpl.Execute(hd.wc, template.JS(hd.buf.String()))
	}
	if cerr := hd.wc.Close(); err == nil {
		err = cerr
	}
	return err
}

type webDevDumper struct {
	sessionWriter
	rollup *exec.Cmd
	buf    bytes.Buffer
	wds    webDevServer
}

type webDevServer struct {
	fs       http.Handler
	tmplFile string
	mtime    time.Time
	data     []byte
}

func newWebDevDumper(dir string, tmplName string) (sessionWriter, error) {
	rollupPath, err := exec.LookPath("rollup")
	if err == nil {
		rollupPath, err = filepath.Abs(rollupPath)
	}
	if err != nil {
		return nil, fmt.Errorf("no rollup: %v", err)
	}

	var wdd webDevDumper
	wdd.sessionWriter = newJSONDumper(nopWriteCloser{&wdd.buf})
	wdd.wds.fs = http.FileServer(http.Dir(dir))
	wdd.wds.tmplFile = path.Join(dir, tmplName)

	wdd.rollup = exec.Command(rollupPath, "-c", path.Join(dir, "rollup.config.js"), "-w")
	wdd.rollup.Env = append(os.Environ(), "ROLLUP_DEV=1")
	wdd.rollup.Dir = path.Clean(path.Join(dir, ".."))
	wdd.rollup.Stdout = os.Stdout
	wdd.rollup.Stderr = os.Stderr

	return &wdd, nil
}

func (wdd *webDevDumper) Close() error {
	err := wdd.sessionWriter.Close()
	if err != nil {
		return err
	}

	wdd.wds.mtime = time.Now()
	wdd.wds.data = wdd.buf.Bytes()

	if err := wdd.rollup.Start(); err != nil {
		return fmt.Errorf("failed to start rollup: %v", err)
	}
	defer func() {
		if wdd.rollup.Process != nil {
			wdd.rollup.Process.Kill()
		}
	}()

	go func() {
		if err := wdd.rollup.Wait(); err != nil {
			log.Fatalf("rollup failed: %v", err)
		}
	}()

	return wdd.wds.run()
}

func (wds webDevServer) run() error {
	http.HandleFunc("/index.html", wds.serveIndex)
	http.HandleFunc("/data.json", wds.serveData)
	http.HandleFunc("/", wds.serveRoot)
	log.Printf("web dev serving on %s", webDevAddr)
	return http.ListenAndServe(webDevAddr, nil)
}

func (wds webDevServer) serveRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/index.html", http.StatusFound)
		return
	}
	wds.fs.ServeHTTP(w, r)
}

func (wds webDevServer) serveIndex(w http.ResponseWriter, r *http.Request) {
	data := template.JS(`fetch("/data.json").then(resp => resp.json())`)
	tmpl, err := template.ParseFiles(wds.tmplFile)
	if err == nil {
		err = tmpl.Execute(w, data)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (wds webDevServer) serveData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	http.ServeContent(w, r, "data.json", wds.mtime, bytes.NewReader(wds.data))
}

type webDumper struct {
	sessionWriter
	temp *os.File
}

func newWebDumper(name string) (sessionWriter, error) {
	webfile, err := ioutil.TempFile("", "stackvm-"+name)
	if err != nil {
		return nil, err
	}
	sw, err := newHTMLDumper(name, webfile)
	if err != nil {
		return nil, err
	}
	return webDumper{
		sessionWriter: sw,
		temp:          webfile,
	}, nil
}

func (wd webDumper) Close() error {
	if err := wd.sessionWriter.Close(); err != nil {
		return err
	}
	var args []string
	switch runtime.GOOS {
	case "darwin":
		args = []string{"open"}
	case "windows":
		args = []string{"cmd", "/c", "start"}
	default:
		args = []string{"xdg-open"}
	}
	args = append(args, wd.temp.Name())
	return exec.Command(args[0], args[1:]...).Start()
}

type sessionWriter interface {
	WriteSession(sessions, machID) error
	Close() error
}

type sessionWriterFunc func(sessions, machID) error

func (swf sessionWriterFunc) WriteSession(ss sessions, mid machID) error { return swf(ss, mid) }
func (swf sessionWriterFunc) Close() error                               { return nil }

//go:generate ../../node_modules/.bin/rollup -c assets/rollup.config.js
//go:generate mkdir -p ./assets/smashed
//go:generate ../../node_modules/.bin/html-inline -b assets -i assets/sunburst.tmpl -o assets/smashed/sunburst.tmpl
//go:generate go-bindata -prefix ./assets/smashed/ ./assets/smashed/

func parseTemplateAsset(name string) (tmpl *template.Template, err error) {
	var content []byte
	tmpl = template.New(name)
	content, err = Asset(name + ".tmpl")
	if err == nil {
		tmpl, err = tmpl.Parse(string(content))
	}
	return
}

var webDevAddr = ":8000"

func main() {
	var (
		terse     bool
		fmtJSON   bool
		fmtHTML   bool
		fmtWeb    bool
		fmtWebDev bool
		ignCodes  = make(intsetFlag)
	)

	flag.BoolVar(&terse, "terse", false, "don't print full session logs")
	flag.Var(ignCodes, "ignoreHaltCodes", "skip printing logs for session that halted with these non-zero codes")
	flag.BoolVar(&fmtJSON, "json", false, "output json")
	flag.BoolVar(&fmtHTML, "html", false, "output html")
	flag.BoolVar(&fmtWeb, "web", false, "open html in browser")
	flag.BoolVar(&fmtWebDev, "web-dev", false, "host html for development")
	flag.StringVar(&webDevAddr, "web-dev-addr", webDevAddr, "listen address for -web-dev server")
	flag.Parse()

	var sw sessionWriter = sessionWriterFunc(printFullSession)

	if fmtJSON {
		sw = newJSONDumper(os.Stdout)
	} else if fmtHTML {
		var err error
		sw, err = newHTMLDumper("sunburst", os.Stdout)
		if err != nil {
			log.Fatal(err)
		}
	} else if fmtWeb {
		var err error
		sw, err = newWebDumper("sunburst")
		if err != nil {
			log.Fatal(err)
		}
	} else if fmtWebDev {
		exe, err := os.Executable()
		if err == nil {
			sw, err = newWebDevDumper(
				path.Join(path.Dir(exe), "tools/tracelog/assets"),
				"sunburst.tmpl")
		}
		if err != nil {
			log.Fatal(err)
		}
	} else if terse {
		sw = sessionWriterFunc(printSession)
	}

	sessions, err := parseSessions(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	mids := make([]machID, 0, len(sessions))
	for mid, sess := range sessions {
		if match := haltPat.FindStringSubmatch(sess.err); match != nil {
			code, _ := strconv.Atoi(match[1])
			if _, ignored := ignCodes[code]; ignored {
				continue
			}
		}
		mids = append(mids, mid)
	}
	sort.Slice(mids, func(i, j int) bool {
		return mids[i][0] < mids[j][0] ||
			mids[i][1] < mids[j][1] ||
			mids[i][2] < mids[j][2]
	})

	if err := func() (err error) {
		for _, mid := range mids {
			if err = sw.WriteSession(sessions, mid); err != nil {
				break
			}
		}
		if cerr := sw.Close(); err == nil {
			err = cerr
		}
		return err
	}(); err != nil {
		log.Fatal(err)
	}
}
