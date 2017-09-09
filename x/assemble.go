package xstackvm

import (
	"fmt"
	"sort"

	"github.com/jcorbin/stackvm"
)

// MustAssemble uses assemble the input, using Assemble(), and panics
// if it returns a non-nil error.
func MustAssemble(in ...interface{}) []byte {
	prog, err := Assemble(in...)
	if err != nil {
		panic(err)
	}
	return prog
}

// Assemble builds a byte encoded machine program from a slice of
// operation names. Operations may be preceded by an immediate
// argument. An immediate argument may be an integer value, or a label
// reference string of the form ":name". Labels are defined with a string of
// the form "name:".
func Assemble(in ...interface{}) ([]byte, error) {
	return NewAssembler().Assemble(in...)
}

// NewAssembler creates a new Assembler with optional options.
func NewAssembler(opts ...Option) Assembler {
	asm := assembler{}
	return asm.With(opts...)
}

// Assembler will assemble a stream of generic tokens into machine code in a
// byte slice.
type Assembler interface {
	With(opts ...Option) Assembler
	Assemble(in ...interface{}) ([]byte, error)
}

// Option is an opaque customization for an Assembler; it is not to be confused
// with a machine option.
type Option func(*assembler)

// Logf sets a debug logging function to an Assembler.
func Logf(f func(string, ...interface{})) Option {
	return func(asm *assembler) {
		asm.logff = f
	}
}

// copied from generated op_codes.go, which isn't that bad since having "the
// zero op crash" should perhaps be the most stable part of the ISA.
const opCodeCrash = 0x00

// copied from api.go.
const optCodeEnd = 0x00

type tokenKind uint8

const (
	optTK tokenKind = iota + 1
	opTK
	dataTK
	allocTK
	stringTK
)

type token struct {
	kind tokenKind
	str  string
	stackvm.Op
}

func (tok token) ResolveRefArg(site, targ uint32) token {
	switch tok.kind {
	case optTK:
		tok.Arg = targ
	case opTK:
		tok.Op = tok.Op.ResolveRefArg(site, targ)
	}
	return tok
}

func (tok token) Name() string {
	switch tok.kind {
	case optTK:
		return stackvm.NameOption(tok.Code)
	case opTK:
		return tok.Op.Name()
	case dataTK:
		return ".data"
	case allocTK:
		return ".alloc"
	case stringTK:
		return ".string"
	default:
		return fmt.Sprintf("UNKNOWN<%v>", tok.kind)
	}
}

func (tok token) String() string {
	switch tok.kind {
	case optTK:
		if tok.Have {
			return fmt.Sprintf(".%s %v", stackvm.NameOption(tok.Code), tok.Arg)
		}
		return fmt.Sprintf(".%s", stackvm.NameOption(tok.Code))
	case opTK:
		return tok.Op.String()
	case dataTK:
		return fmt.Sprintf(".data %d", tok.Arg)
	case allocTK:
		return fmt.Sprintf(".alloc %d", tok.Arg)
	case stringTK:
		return fmt.Sprintf(".string %q", tok.str)
	default:
		return fmt.Sprintf("UNKNOWN<%v>", tok.kind)
	}
}

func (tok token) EncodeInto(p []byte) int {
	switch tok.kind {
	case dataTK:
		stackvm.ByteOrder.PutUint32(p, tok.Arg)
		return 4
	case allocTK:
		n := 0
		for i := uint32(0); i < tok.Arg; i++ {
			p[n] = 0
			n++
			p[n] = 0
			n++
			p[n] = 0
			n++
			p[n] = 0
			n++
		}
		return n
	case stringTK:
		n := 0
		stackvm.ByteOrder.PutUint32(p, uint32(len(tok.str)))
		n += 4
		for i := 0; i < len(tok.str); i++ {
			p[n] = tok.str[i]
			n++
		}
		return n
	default:
		return tok.Op.EncodeInto(p)
	}
}

func (tok token) NeededSize() int {
	switch tok.kind {
	case dataTK:
		return 4
	case allocTK:
		return 4 * int(tok.Arg)
	case stringTK:
		return 4 + len(tok.str)
	default:
		return tok.Op.NeededSize()
	}
}

func optToken(name string, arg uint32, have bool) token {
	return token{
		kind: optTK,
		Op:   stackvm.ResolveOption(name, arg, have),
	}
}

func opToken(op stackvm.Op) token { return token{kind: opTK, Op: op} }
func dataToken(d uint32) token    { return token{kind: dataTK, Op: stackvm.Op{Arg: d}} }
func allocToken(n uint32) token   { return token{kind: allocTK, Op: stackvm.Op{Arg: n}} }
func stringToken(s string) token  { return token{kind: stringTK, str: s} }

type ref struct{ site, targ, off int }

type assembler struct {
	logff func(string, ...interface{})

	pendIn, pendOut string

	opts, prog section

	stackSize *token
	queueSize *token
	maxOps    *token
	maxCopies *token
}

func (asm assembler) Assemble(in ...interface{}) ([]byte, error) {
	asm.opts = makeSection()
	asm.prog = makeSection()

	asm.addOpt("version", 0, false)
	asm.stackSize = asm.refOpt("stackSize", defaultStackSize, true)

	if err := asm.scan(in); err != nil {
		return nil, err
	}

	enc, err := asm.finish()
	if err != nil {
		return nil, err
	}

	return enc.encode()
}

func (asm assembler) With(opts ...Option) Assembler {
	for _, opt := range opts {
		opt(&asm)
	}
	return asm
}

func (asm *assembler) logf(format string, args ...interface{}) {
	if asm.logff != nil {
		asm.logff(format, args...)
	}
}

func (asm *assembler) setOption(ptok **token, name string, v uint32) {
	if *ptok == nil {
		*ptok = asm.refOpt(name, v, true)
	} else {
		(*ptok).Arg = v
	}
}

type section struct {
	toks     []token
	refsBy   map[string][]ref
	labels   map[string]int
	maxBytes int
}

func makeSection() section {
	return section{
		toks:     nil,
		refsBy:   make(map[string][]ref),
		labels:   make(map[string]int),
		maxBytes: 0,
	}
}

func collectSections(secs ...section) (enc encoder, err error) {
	numLabels, numRefsBy, numRefs, numToks := 0, 0, 0, 0
	for _, sec := range secs {
		numToks += len(sec.toks)
		numRefsBy += len(sec.refsBy)
		numLabels += len(sec.labels)
		enc.maxBytes += sec.maxBytes
		for _, rfs := range sec.refsBy {
			numRefs += len(rfs)
		}
	}
	if numToks > 0 {
		enc.toks = make([]token, 0, numToks)
	}
	if numRefsBy > 0 {
		enc.refsBy = make(map[string][]ref, numRefsBy)
	}
	if numLabels > 0 {
		enc.labels = make(map[string]int)
	}
	if numRefs > 0 {
		enc.refs = make([]ref, 0, numRefs)
	}

	base := 0
	for _, sec := range secs {
		// collect tokens
		enc.toks = append(enc.toks, sec.toks...)

		// collect refsBy
		for name, rfs := range sec.refsBy {
			crfs := enc.refsBy[name]
			for _, rf := range rfs {
				rf.site += base
				crfs = append(crfs, rf)
			}
			enc.refsBy[name] = crfs
		}

		// collect labels
		for name, off := range sec.labels {
			if off >= 0 {
				enc.labels[name] = base + off
			}
		}

		base += len(sec.toks)
	}

	// check for undefined label refs
	var undefined []string
	for name := range enc.refsBy {
		if i, defined := enc.labels[name]; !defined || i < 0 {
			undefined = append(undefined, name)
		}
	}
	if len(undefined) > 0 {
		err = fmt.Errorf("undefined labels: %q", undefined)
		return
	}

	// resolve refs
	for name, rfs := range enc.refsBy {
		targ := enc.labels[name]
		for _, rf := range rfs {
			rf.targ = targ
			enc.refs = append(enc.refs, rf)
		}
	}

	if len(enc.refs) > 0 {
		sort.Slice(enc.refs, func(i, j int) bool {
			return enc.refs[i].site < enc.refs[j].site
		})
	}

	return
}

func (sec *section) add(tok token) {
	sec.toks = append(sec.toks, tok)
	sec.maxBytes += tok.NeededSize()
}

func (sec *section) addRef(tok token, name string, off int) {
	rf := ref{site: len(sec.toks), off: off}
	sec.refsBy[name] = append(sec.refsBy[name], rf)
	sec.toks = append(sec.toks, tok)
	sec.maxBytes += 6
}

func (sec *section) stubLabel(name string) {
	if _, defined := sec.labels[name]; !defined {
		sec.labels[name] = -1
	}
}

func (sec *section) addLabel(name string) {
	sec.labels[name] = len(sec.toks)
}

type assemblerState uint8

const (
	assemblerText assemblerState = iota + 1
	assemblerData
)

const defaultStackSize = 0x40

func (asm *assembler) refOpt(name string, arg uint32, have bool) *token {
	i := len(asm.opts.toks)
	asm.addOpt(name, arg, have)
	return &asm.opts.toks[i]
}

func (asm *assembler) addOpt(name string, arg uint32, have bool) {
	asm.opts.add(optToken(name, arg, have))
}

func (asm *assembler) addRefOpt(name string, targetName string, off int) {
	tok := optToken(name, 0, true)
	asm.opts.addRef(tok, targetName, off)
}

func (asm *assembler) scan(in []interface{}) error {
	sc := scanner{assembler: asm}
	return sc.scan(in)
}

func (asm *assembler) finish() (encoder, error) {
	// finish options
	asm.addOpt("end", 0, false)

	// build encoder with all assembled state
	enc, err := collectSections(asm.opts, asm.prog)
	enc.logf = asm.logf
	enc.base = asm.stackSize.Arg
	return enc, err
}

type scanner struct {
	*assembler
	prior []scannerState
	scannerState
}

type scannerState struct {
	i     int
	in    []interface{}
	state assemblerState
}

func (sc *scanner) scan(in []interface{}) error {
	sc.scannerState = scannerState{
		i:     0,
		in:    in,
		state: assemblerText,
	}
	for {
		for ; sc.i < len(sc.in); sc.i++ {
			switch sc.state {
			case assemblerData:
				if err := sc.handleData(sc.in[sc.i]); err != nil {
					return err
				}
			case assemblerText:
				if err := sc.handleText(sc.in[sc.i]); err != nil {
					return err
				}
			default:
				return fmt.Errorf("invalid assembler state %d", sc.state)
			}
		}
		if i := len(sc.prior) - 1; i >= 0 {
			sc.scannerState, sc.prior = sc.prior[i], sc.prior[:i]
			sc.i++
			continue
		}
		return nil
	}
}

func (sc *scanner) handleQueueSize() error {
	n, err := sc.expectInt("queueSize int")
	if err != nil {
		return err
	}
	if n < 0 {
		return fmt.Errorf("invalid .queueSize %v, must be non-negative", n)
	}
	sc.setOption(&sc.queueSize, "queueSize", uint32(n))
	return nil
}

func (sc *scanner) handleMaxOps() error {
	n, err := sc.expectInt("maxOps int")
	if err != nil {
		return err
	}
	if n < 0 {
		return fmt.Errorf("invalid .maxOps %v, must be non-negative", n)
	}
	sc.setOption(&sc.maxOps, "maxOps", uint32(n))
	return nil
}

func (sc *scanner) handleMaxCopies() error {
	n, err := sc.expectInt("maxCopies int")
	if err != nil {
		return err
	}
	if n < 0 {
		return fmt.Errorf("invalid .maxCopies %v, must be non-negative", n)
	}
	sc.setOption(&sc.maxCopies, "maxCopies", uint32(n))
	return nil
}

func (sc *scanner) handleStackSize() error {
	n, err := sc.expectInt("stackSize int")
	if err != nil {
		return err
	}
	if n < +0 || n > 0xffff {
		return fmt.Errorf("stackSize %d out of range, must be in (0x0000, 0xffff)", n)
	}
	sc.stackSize.Arg = uint32(n)
	return nil
}

func (sc *scanner) handleData(val interface{}) error {
	switch v := val.(type) {
	case string:
		switch {
		case len(v) > 1 && v[0] == '.':
			switch s := v[1:]; s {
			case "alloc":
				return sc.handleAlloc()
			case "in":
				return sc.handleInput()
			case "out":
				return sc.handleOutput()
			default:
				return sc.handleDirective(s)
			}

		case len(v) > 1 && v[len(v)-1] == ':':
			return sc.handleLabel(v[:len(v)-1])

		default:
			return fmt.Errorf("unexpected string %q", v)
		}

	case int:
		return sc.handleDataWord(uint32(v))

	default:
		return fmt.Errorf(`invalid token %T(%v); expected ".directive", "label:", or an int`, val, val)
	}
}

func (sc *scanner) handleText(val interface{}) error {
	switch v := val.(type) {
	case string:
		switch {
		case len(v) > 1 && v[0] == '.':
			return sc.handleDirective(v[1:])

		case len(v) > 1 && v[len(v)-1] == ':':
			return sc.handleLabel(v[:len(v)-1])

		case len(v) > 1 && v[0] == ':':
			return sc.handleRef(v[1:])

		default:
			return sc.handleOp(v)
		}

	case int:
		return sc.handleImm(v)

	default:
		return fmt.Errorf(`invalid token %T(%v); expected ".directive", "label:", ":ref", "opName", or an int`, val, val)
	}
}

func (sc *scanner) handleDirective(name string) error {
	switch name {
	case "entry":
		return sc.handleEntry()
	case "stackSize":
		return sc.handleStackSize()
	case "queueSize":
		return sc.handleQueueSize()
	case "maxOps":
		return sc.handleMaxOps()
	case "maxCopies":
		return sc.handleMaxCopies()
	case "data":
		return sc.setState(assemblerData)
	case "text":
		return sc.setState(assemblerText)
	case "include":
		return sc.handleInclude()
	default:
		return fmt.Errorf("invalid directive .%s", name)
	}
}

func (sc *scanner) handleInclude() error {
	val, err := sc.expect("[]interface{}")
	if err != nil {
		return err
	}
	subProg, ok := val.([]interface{})
	if !ok {
		return fmt.Errorf("invalid token %T(%v); expected []interface{}", val, val)
	}
	sc.prior, sc.scannerState = append(sc.prior, sc.scannerState), scannerState{
		i:     -1, // TODO: because of how the loop in sc.scan works, bit regrettable
		in:    subProg,
		state: assemblerText,
	}
	return nil
}

func (sc *scanner) setState(state assemblerState) error {
	if sc.state == state {
		return nil
	}
	sc.state = state
	switch state {
	case assemblerText:
		if sc.pendIn != "" {
			return sc.finishIn()
		}
		if sc.pendOut != "" {
			return sc.finishOut()
		}
	}
	return nil
}

func (sc *scanner) handleEntry() error {
	s, err := sc.expectString(`"label:"`)
	if err != nil {
		return err
	}

	// expect and define label
	if len(s) < 2 || s[len(s)-1] != ':' {
		return fmt.Errorf("unexpected string %q, expected .entry label", s)
	}
	name := s[:len(s)-1]
	if err := sc.handleLabel(name); err != nil {
		return err
	}

	// dupe check .entry
	if i, defined := sc.prog.labels[".entry"]; defined && i >= 0 {
		for dupName, j := range sc.prog.labels {
			if j == i {
				return fmt.Errorf("duplicate .entry %q, already set to %q", name, dupName)
			}
		}
		return fmt.Errorf("duplicate .entry %q, already set to ???", name)
	}
	sc.prog.addLabel(".entry")
	sc.addRefOpt("entry", name, 0)

	return sc.setState(assemblerText)
}

func (sc *scanner) handleLabel(name string) error {
	if sc.pendIn != "" {
		if err := sc.finishIn(); err != nil {
			return err
		}
	}

	if sc.pendOut != "" {
		if err := sc.finishOut(); err != nil {
			return err
		}
	}

	if i, defined := sc.prog.labels[name]; defined && i >= 0 {
		return fmt.Errorf("label %q already defined", name)
	}
	sc.prog.addLabel(name)
	return nil
}

func (sc *scanner) finishIn() error {
	nameLabel := fmt.Sprintf(".%s.name", sc.pendIn)
	endLabel := "." + sc.pendIn + ".end"
	if i, defined := sc.prog.labels[endLabel]; defined && i >= 0 {
		return fmt.Errorf("label %q already defined", endLabel)
	}
	sc.prog.addLabel(endLabel)
	sc.addRefOpt("input", sc.pendIn, 0)
	sc.addRefOpt("input", endLabel, 0)
	sc.addRefOpt("name", nameLabel, 0)
	sc.prog.addLabel(nameLabel)
	sc.prog.add(stringToken(sc.pendIn))
	sc.pendIn = ""
	return nil
}

func (sc *scanner) finishOut() error {
	nameLabel := fmt.Sprintf(".%s.name", sc.pendOut)
	endLabel := "." + sc.pendOut + ".end"
	if i, defined := sc.prog.labels[endLabel]; defined && i >= 0 {
		return fmt.Errorf("label %q already defined", endLabel)
	}
	sc.prog.addLabel(endLabel)
	sc.addRefOpt("output", sc.pendOut, 0)
	sc.addRefOpt("output", endLabel, 0)
	sc.addRefOpt("name", nameLabel, 0)
	sc.prog.addLabel(nameLabel)
	sc.prog.add(stringToken(sc.pendOut))
	sc.pendOut = ""
	return nil
}

func (sc *scanner) handleRef(name string) error {
	tok, err := sc.expectRefOp(0, true, name)
	if err != nil {
		return err
	}
	sc.prog.addRef(tok, name, 0)
	sc.refLabel(name)
	return nil
}

func (sc *scanner) handleOffRef(name string, n int) error {
	tok, err := sc.expectRefOp(0, true, name)
	if err != nil {
		return err
	}
	sc.prog.addRef(tok, name, n)
	sc.refLabel(name)
	return nil
}

func (sc *scanner) handleOp(name string) error {
	op, err := stackvm.ResolveOp(name, 0, false)
	if err != nil {
		return err
	}
	sc.prog.add(opToken(op))
	return nil
}

func (sc *scanner) handleImm(n int) error {
	s, err := sc.expectString(`":ref" or "opName"`)
	if err != nil {
		return err
	}
	if len(s) > 1 && s[0] == ':' {
		return sc.handleOffRef(s[1:], n)
	}
	op, err := stackvm.ResolveOp(s, uint32(n), true)
	if err != nil {
		return err
	}
	sc.prog.add(opToken(op))
	return nil
}

func (sc *scanner) handleAlloc() error {
	n, err := sc.expectInt(`int`)
	if err != nil {
		return err
	}
	if n < 1 {
		return fmt.Errorf("invalid .alloc %v, must be positive", n)
	}
	// TODO: should be in bytes, not words
	sc.prog.add(allocToken(uint32(n)))
	return nil
}

func (sc *scanner) handleInput() error {
	s, err := sc.expectString(`"label:"`)
	if err != nil {
		return err
	}

	// expect and define label
	if len(s) < 2 || s[len(s)-1] != ':' {
		return fmt.Errorf("unexpected string %q, expected .in label", s)
	}
	name := s[:len(s)-1]
	if err := sc.handleLabel(name); err != nil {
		return err
	}

	// stash name to be flushed by handleLabel
	sc.pendIn = name
	return nil
}

func (sc *scanner) handleOutput() error {
	s, err := sc.expectString(`"label:"`)
	if err != nil {
		return err
	}

	// expect and define label
	if len(s) < 2 || s[len(s)-1] != ':' {
		return fmt.Errorf("unexpected string %q, expected .out label", s)
	}
	name := s[:len(s)-1]
	if err := sc.handleLabel(name); err != nil {
		return err
	}

	// stash name to be flushed by handleLabel
	sc.pendOut = name
	return nil
}

func (sc *scanner) handleDataWord(d uint32) error {
	sc.prog.add(dataToken(d))
	return nil
}

func (sc *scanner) expectRefOp(arg uint32, have bool, name string) (token, error) {
	opName, err := sc.expectString(`"opName"`)
	if err != nil {
		return token{}, err
	}
	op, err := stackvm.ResolveOp(opName, arg, have)
	if err != nil {
		return token{}, err
	}
	if !op.AcceptsRef() {
		return token{}, fmt.Errorf("%v does not accept ref %q", op, name)
	}
	return opToken(op), nil
}

func (sc *scanner) expectOp(arg uint32, have bool) (token, error) {
	opName, err := sc.expectString(`"opName"`)
	if err != nil {
		return token{}, err
	}
	op, err := stackvm.ResolveOp(opName, arg, have)
	if err != nil {
		return token{}, err
	}
	return opToken(op), nil
}

func (sc *scanner) expectString(desc string) (string, error) {
	val, err := sc.expect(desc)
	if err == nil {
		if s, ok := val.(string); ok {
			return s, nil
		}
		err = fmt.Errorf("invalid token %T(%v); expected %s", val, val, desc)
	}
	return "", err
}

func (sc *scanner) expectInt(desc string) (int, error) {
	val, err := sc.expect(desc)
	if err == nil {
		if n, ok := val.(int); ok {
			return n, nil
		}
		err = fmt.Errorf("invalid token %T(%v); expected %s", val, val, desc)
	}
	return 0, err
}

func (sc *scanner) expect(desc string) (interface{}, error) {
	sc.i++
	if sc.i < len(sc.in) {
		return sc.in[sc.i], nil
	}
	return nil, fmt.Errorf("unexpected end of input, expected %s", desc)
}

func (asm *assembler) refLabel(name string) {
	asm.prog.stubLabel(name)
}

type encoder struct {
	section
	logf func(string, ...interface{})
	base uint32
	refs []ref
}

func (enc encoder) encode() ([]byte, error) {
	var (
		buf     = make([]byte, enc.maxBytes)
		offsets = make([]uint32, len(enc.toks)+1)
		boff    uint32           // offset of encoded program
		c       uint32           // current token offset
		nopts   int              // count of option tokens
		i       int              // current token index
		rfi     int              // index of next ref
		rf      = ref{-1, -1, 0} // next ref
	)

	if len(enc.refs) > 0 {
		rf = enc.refs[rfi]
	}

	// encode options
encodeOptions:
	nopts = 0
	for i < len(enc.toks) {
		tok := enc.toks[i]
		c += uint32(tok.EncodeInto(buf[c:]))
		i++
		offsets[i] = c
		if tok.Code == optCodeEnd {
			break
		}
	}
	nopts, boff = i, c

	// encode program
	for i < len(enc.toks) {
		// fix a previously encoded ref's target
		for 0 <= rf.site && rf.site < i && rf.targ <= i {
			// re-encode the ref and rewind if arg size changed
			lo, hi := offsets[rf.site], offsets[rf.site+1]
			site := enc.base + offsets[rf.site] - boff
			targ := enc.base + offsets[rf.targ] - boff + uint32(enc.refs[rfi].off)
			tok := enc.toks[rf.site]
			tok = tok.ResolveRefArg(site, targ)
			enc.toks[rf.site] = tok
			if end := lo + uint32(tok.EncodeInto(buf[lo:])); end != hi {
				// rewind to prior ref
				i, c = rf.site+1, end
				offsets[i] = c
				for rfi, rf = range enc.refs {
					if rf.site >= i || rf.targ >= i {
						break
					}
				}
				if i < nopts {
					goto encodeOptions
				}
			} else {
				// next ref
				rfi++
				if rfi >= len(enc.refs) {
					rf = ref{site: -1, targ: -1}
				} else {
					rf = enc.refs[rfi]
				}
			}
		}

		// encode next token
		tok := enc.toks[i]
		c += uint32(tok.EncodeInto(buf[c:]))
		i++
		offsets[i] = c
	}

	if rf.site >= 0 {
		tok := enc.toks[rf.site]
		name := "???"
		for n, targ := range enc.labels {
			if targ == rf.targ {
				name = n
			}
		}
		if rf.off != 0 {
			return nil, fmt.Errorf("unresolved reference for `%d, \":%s\", %q`", rf.off, name, tok.Name())
		}
		return nil, fmt.Errorf("unresolved reference for `\":%s\", %q`", name, tok.Name())
	}

	return buf[:c], nil
}
