package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
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
)

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
			ss.session(sess.pid).addCoCopyRec(rec)
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
		`^\+\+\+ +(\d+)\((\d+):(\d+)\) +copy` +
		`)|(` +
		`=== +Begin` + // @0x00ce stacks=[0x0000:0x003c]
		`)|(` +
		`^=== +End` +
		`)|(` +
		`^=== +Handle` +
		`)`)

	kvPat = regexp.MustCompile(`^(\w+)=(.+)`)
)

func (sess *session) add(rec record) record {
	switch amatch := actPat.FindStringSubmatch(rec.act); {
	case amatch == nil:
	case amatch[1] != "": // copy
		rec.kind = copyLine
		sess.pid[0], _ = strconv.Atoi(amatch[2])
		sess.pid[1], _ = strconv.Atoi(amatch[3])
		sess.pid[2], _ = strconv.Atoi(amatch[4])

	case amatch[5] != "": // begin
		rec.kind = beginLine

	case amatch[6] != "": // end
		rec.kind = endLine
		if match := kvPat.FindStringSubmatch(rec.rest); match != nil {
			switch string(match[1]) {
			case "err":
				sess.err = string(match[2])
			case "values":
				sess.values = string(match[2])
			default:
				log.Printf("UNKNOWN End key/val: %q = %q\n", match[1], match[2])
			}
		}

	case amatch[7] != "": // handle
		rec.kind = hndlLine

	default:
		rec.kind = genericLine
	}

	sess.recs = append(sess.recs, rec)
	return rec
}

func (sess *session) addCoCopyRec(rec record) {
	sess.recs = append(sess.recs, record{
		kind:  copyLine,
		mid:   sess.pid,
		cid:   rec.mid,
		count: rec.count,
		act:   fmt.Sprintf("+++ %*v", 15, fmt.Sprintf("%v copy", rec.mid)),
	})
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

func (ss sessions) sessionLog(sess *session, logf func(string, ...interface{})) {
	ids := ss.idPath(sess)
	for i, j := 0, 1; j < len(ids); i, j = i+1, j+1 {
		sess := ss[ids[i]]
		for _, rec := range sess.recs {
			if rec.kind == copyLine && rec.cid == ids[j] {
				break
			}
			logf("%v", rec)
		}
	}
	for _, rec := range sess.recs {
		logf("%v", rec)
	}
	for _, line := range sess.extra {
		logf("%s", line)
	}
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

func main() {
	var (
		terse    bool
		ignCodes = make(intsetFlag)
	)

	flag.BoolVar(&terse, "terse", false, "don't print full session logs")
	flag.Var(ignCodes, "ignoreHaltCodes", "skip printing logs for session that halted with these non-zero codes")
	flag.Parse()

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
	for _, mid := range mids {
		sess := sessions[mid]
		if sess.err != "" {
			fmt.Printf("%s\terr=%v\n", sessions.fullID(sess), sess.err)
		} else {
			fmt.Printf("%s\tvalues=%v\n", sessions.fullID(sess), sess.values)
		}
		if !terse {
			sessions.sessionLog(sess, func(format string, args ...interface{}) {
				fmt.Printf("	"+format+"\n", args...)
			})
			fmt.Println()
		}
	}
}
