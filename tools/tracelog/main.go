package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
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

var midLogPat = regexp.MustCompile(`\w+\.go:\d+: +(\d+)\((\d+):(\d+)\) +(.+)`)

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
	actPat = regexp.MustCompile(`(` +
		`^\+\+\+ +Copy` +
		`)|(` +
		`=== +Begin` + // @0x00ce stacks=[0x0000:0x003c]
		`)|(` +
		`^=== +End` +
		`)|(` +
		`^=== +Handle` +
		`)`)

	markPat = regexp.MustCompile(`^(\+\+\+|===|\.\.\.|>>>)\s*`)

	midPat = regexp.MustCompile(`(\d+)\((\d+):(\d+)\)`)
)

func scanKVs(s string, each func(k, v string)) {
	ks, ke, vs, ve := 0, 0, 0, 0
	bc, cc, pc := 0, 0, 0

seekKey:
	for ; ks < len(s); ks++ {
		switch s[ks] {
		case ' ', '\t', '\n':
		default:
			goto scanKey
		}
	}
	return

scanKey:
	for ke = ks; ke < len(s); ke++ {
		switch s[ke] {
		case ' ', '\t', '\n':
			ks = ke
			goto seekKey
		case '=':
			goto seekVal
		}
	}
	return

seekVal:
	vs = ke + 1
	ve = vs

scanVal:
	for ; ve < len(s); ve++ {
		switch s[ve] {
		case '"':
			goto scanDQ
		case '\'':
			goto scanSQ

		case '[':
			bc++
		case ']':
			if bc > 0 {
				bc--
			}

		case '{':
			cc++
		case '}':
			if cc > 0 {
				cc--
			}

		case '(':
			pc++
		case ')':
			if pc > 0 {
				pc--
			}

		case ' ', '\t', '\n':
			if bc+cc+pc <= 0 {
				goto emit
			}
		}
	}
	goto emit

scanDQ:
	for ; ve < len(s); ve++ {
		switch s[ve] {
		case '\\':
			ve++
		case '"':
			ve++
			goto scanVal
		}
	}
	goto emit

scanSQ:
	for ; ve < len(s); ve++ {
		switch s[ve] {
		case '\\':
			ve++
		case '\'':
			ve++
			goto scanVal
		}
	}
	goto emit

emit:
	each(s[ks:ke], s[vs:ve])
	ks = ve + 1
	if ks < len(s) {
		goto scanKey
	}
}

func (sess *session) add(rec record) record {
	switch amatch := actPat.FindStringSubmatch(rec.act); {
	case amatch == nil:
	case amatch[1] != "": // copy
		rec.kind = copyLine
		var parts []string
		scanKVs(rec.rest, func(k, v string) {
			switch k {
			case "child":
				if match := midPat.FindStringSubmatch(v); match != nil {
					rec.cid[0], _ = strconv.Atoi(match[1])
					rec.cid[1], _ = strconv.Atoi(match[2])
					rec.cid[2], _ = strconv.Atoi(match[3])
					parts = append(parts, fmt.Sprintf("child=%v", rec.cid))
				}
			default:
				parts = append(parts, fmt.Sprintf("%s=%s", k, v))
			}
		})
		rec.rest = strings.Join(parts, " ")

	case amatch[2] != "": // begin
		rec.kind = beginLine

	case amatch[3] != "": // end
		rec.kind = endLine
		scanKVs(rec.rest, func(k, v string) {
			switch k {
			case "err":
				sess.err = v
			case "values":
				sess.values = v
			default:
				log.Printf("UNKNOWN End key/val: %q = %q\n", k, v)
			}
		})

	case amatch[4] != "": // handle
		rec.kind = hndlLine

	default:
		rec.kind = genericLine
	}

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
	values   string
	extra    []string
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
		sess = &session{mid: mid}
		ss[mid] = sess
	}
	return sess
}

func (ss sessions) extend(mid machID, s string) {
	sess := ss.session(mid)
	sess.extra = append(sess.extra, s)
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
		}
	}
	for _, rec := range sess.recs {
		if err := logf("%v", rec); err != nil {
			return err
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
		_, err = fmt.Printf("%s\tvalues=%v\n", sessions.fullID(sess), sess.values)
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
}

type sessDat struct {
	ID       string   `json:"id"`
	ParentID *string  `json:"parent_id"`
	Error    string   `json:"error"`
	Values   string   `json:"values"`
	Records  []recDat `json:"records"`
	Extra    []string `json:"extra"`
}

func (sess *session) toJSON() sessDat {
	dat := sessDat{
		ID:      sess.mid.String(),
		Error:   sess.err,
		Values:  sess.values,
		Records: make([]recDat, len(sess.recs)),
		Extra:   sess.extra,
	}

	if sess.pid != zeroMachID {
		pidStr := sess.pid.String()
		dat.ParentID = &pidStr
	}

	for i, rec := range sess.recs {
		// TODO: push this munging back into primary code
		kind, act := rec.kind, rec.act
		if m := markPat.FindStringSubmatchIndex(act); m != nil {
			switch act[m[2]:m[3]] {
			case ">>>":
				kind = preOpLine
			case "...":
				kind = postOpLine
			}
			act = act[m[1]:]
		}
		act = strings.TrimSpace(act)

		rd := recDat{
			Kind:   kind.String(),
			Action: act,
			Count:  rec.count,
			IP:     int(rec.ip),
			Extra:  make(map[string]interface{}),
		}
		if rec.cid != zeroMachID {
			rd.Extra["child"] = rec.cid.String()
		}
		scanKVs(rec.rest, func(k, v string) {
			rd.Extra[k] = v
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

type sessionWriter interface {
	WriteSession(sessions, machID) error
	Close() error
}

type sessionWriterFunc func(sessions, machID) error

func (swf sessionWriterFunc) WriteSession(ss sessions, mid machID) error { return swf(ss, mid) }
func (swf sessionWriterFunc) Close() error                               { return nil }

//go:generate ../../node_modules/.bin/rollup -c
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

func main() {
	var (
		terse    bool
		fmtJSON  bool
		ignCodes = make(intsetFlag)
	)

	flag.BoolVar(&terse, "terse", false, "don't print full session logs")
	flag.Var(ignCodes, "ignoreHaltCodes", "skip printing logs for session that halted with these non-zero codes")
	flag.BoolVar(&fmtJSON, "json", false, "output json")
	flag.Parse()

	var sw sessionWriter = sessionWriterFunc(printFullSession)

	if fmtJSON {
		sw = newJSONDumper(os.Stdout)
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
