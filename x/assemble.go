package xstackvm

import (
	"encoding/binary"
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
	addrLabelTK
)

func (tk tokenKind) String() string {
	switch tk {
	case optTK:
		return "opt"
	case opTK:
		return "op"
	case dataTK:
		return "data"
	case allocTK:
		return "alloc"
	case stringTK:
		return "string"
	case addrLabelTK:
		return "addrLabel"
	default:
		return fmt.Sprintf("invalid<%02x>", uint8(tk))
	}
}

type token struct {
	kind tokenKind
	str  string
	stackvm.Op
}

func (tok token) ResolveRefArg(site, targ uint32) token {
	switch tok.kind {
	case optTK:
		tok.Op = stackvm.ResolveOptionRefArg(tok.Op, site, targ)
	case opTK:
		tok.Op = tok.Op.ResolveRefArg(site, targ)
	case addrLabelTK:
		tok.Arg = targ
	default:
		panic(fmt.Sprintf("%v token does not accept ref args", tok.kind))
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
	case addrLabelTK:
		return fmt.Sprintf("@label:")
	default:
		return fmt.Sprintf("UNKNOWN<%v>", tok.kind)
	}
}

func (tok token) String() string {
	switch tok.kind {
	case optTK:
		var s string
		if tok.Have {
			s = fmt.Sprintf(".%s %v", stackvm.NameOption(tok.Code), tok.Arg)
		} else {
			s = fmt.Sprintf(".%s", stackvm.NameOption(tok.Code))
		}
		if tok.str != "" {
			s = fmt.Sprintf("%s (:%s)", s, tok.str)
		}
		return s
	case opTK:
		s := tok.Op.String()
		if tok.str != "" {
			s = fmt.Sprintf("%s (:%s)", s, tok.str)
		}
		return s
	case dataTK:
		return fmt.Sprintf(".data %d", tok.Arg)
	case allocTK:
		return fmt.Sprintf(".alloc %d", tok.Arg)
	case stringTK:
		return fmt.Sprintf(".string %q", tok.str)
	case addrLabelTK:
		return fmt.Sprintf("@%s:", tok.str)
	default:
		return fmt.Sprintf("UNKNOWN<%v>", tok.kind)
	}
}

func (tok token) EncodeInto(p []byte) int {
	switch tok.kind {
	case optTK, opTK:
		return tok.Op.EncodeInto(p)
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
	case addrLabelTK:
		n := 0
		n += binary.PutUvarint(p[n:], uint64(tok.Arg))
		n += binary.PutUvarint(p[n:], uint64(len(tok.str)))
		for i := 0; i < len(tok.str); i++ {
			p[n] = tok.str[i]
			n++
		}
		return n
	default:
		panic(fmt.Sprintf("invalid token kind %v", tok.kind))
	}
}

func (tok token) NeededSize() int {
	switch tok.kind {
	case optTK:
		return stackvm.OptionNeededSize(tok.Op)
	case opTK:
		return tok.Op.NeededSize()
	case dataTK:
		return 4
	case allocTK:
		return 4 * int(tok.Arg)
	case stringTK:
		return 4 + len(tok.str)
	case addrLabelTK:
		var buf [binary.MaxVarintLen64]byte
		n := binary.MaxVarintLen64
		n += binary.PutUvarint(buf[:], uint64(len(tok.str)))
		n += len(tok.str)
		return n
	default:
		panic(fmt.Sprintf("invalid token kind %v", tok.kind))
	}
}

func optToken(name string, arg uint32, have bool) token {
	return token{
		kind: optTK,
		Op:   stackvm.ResolveOption(name, arg, have),
	}
}

func opToken(op stackvm.Op) token   { return token{kind: opTK, Op: op} }
func dataToken(d uint32) token      { return token{kind: dataTK, Op: stackvm.Op{Arg: d}} }
func allocToken(n uint32) token     { return token{kind: allocTK, Op: stackvm.Op{Arg: n}} }
func stringToken(s string) token    { return token{kind: stringTK, str: s} }
func addrLabelToken(s string) token { return token{kind: addrLabelTK, str: s} }

type ref struct{ site, targ, off int }

type unkRet struct {
	label  string
	cur    int
	labels []string
}

type assembler struct {
	logff func(string, ...interface{})

	pendIn, pendOut string

	adls, opts, prog section
	opens            map[string]struct{}
	unkRets          map[string]*unkRet

	stackSize *token
	queueSize *token
	maxOps    *token
	maxCopies *token
}

func (asm assembler) Assemble(in ...interface{}) ([]byte, error) {
	asm.opts = makeSection()
	asm.prog = makeSection()

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

type namedRef struct {
	targName string
	ref
}

type section struct {
	toks     []token
	refs     []namedRef
	labels   map[string]int
	maxBytes int
}

func makeSection(toks ...token) section {
	sec := section{toks: toks}
	for i := range toks {
		sec.maxBytes += toks[i].NeededSize()
	}
	return sec
}

func collectSections(secs ...section) (sec section) {
	numLabels, numRefs, numToks := 0, 0, 0
	for _, s := range secs {
		numToks += len(s.toks)
		numRefs += len(s.refs)
		numLabels += len(s.labels)
		sec.maxBytes += s.maxBytes
	}
	if numLabels > 0 {
		sec.labels = make(map[string]int)
	}
	if numRefs > 0 {
		sec.refs = make([]namedRef, 0, numRefs)
	}
	if numToks > 0 {
		sec.toks = make([]token, 0, numToks)
	}

	base := 0
	for _, s := range secs {
		for i := range s.refs {
			s.refs[i].site += base
		}
		sec.refs = append(sec.refs, s.refs...)

		for name, off := range s.labels {
			if off >= 0 {
				sec.labels[name] = base + off
			}
		}

		sec.toks = append(sec.toks, s.toks...)

		base += len(s.toks)
	}

	return
}

func (sec section) checkLabels() error {
	var undefined []string
	noted := make(map[string]struct{}, len(sec.refs))
	for _, nrf := range sec.refs {
		if nrf.targName != "" {
			if _, targNoted := noted[nrf.targName]; !targNoted {
				if i, defined := sec.labels[nrf.targName]; !defined || i < 0 {
					undefined = append(undefined, nrf.targName)
				}
			}
		}
	}
	if len(undefined) > 0 {
		return fmt.Errorf("undefined labels: %q", undefined)
	}
	return nil
}

func (sec section) resolveRefs() (refs []ref) {
	numRefs := len(sec.refs)
	if numRefs > 0 {
		refs = make([]ref, 0, numRefs)
	}
	for _, nrf := range sec.refs {
		if nrf.targName == "" {
			panic(fmt.Sprintf("NOPE %v", nrf))
		}
		nrf.targ = sec.labels[nrf.targName]
		refs = append(refs, nrf.ref)
	}
	if len(refs) > 0 {
		sort.Slice(refs, func(i, j int) bool {
			return refs[i].site < refs[j].site
		})
	}
	return
}

func (sec *section) add(tok token) {
	sec.toks = append(sec.toks, tok)
	sec.maxBytes += tok.NeededSize()
}

func (sec *section) addRef(tok token, name string, off int) {
	sec.refs = append(sec.refs, namedRef{name, ref{site: len(sec.toks), off: off}})
	sec.toks = append(sec.toks, tok)
	sec.maxBytes += 6
}

func (sec *section) stubLabel(name string) {
	if sec.labels == nil {
		sec.labels = make(map[string]int)
	}
	if _, defined := sec.labels[name]; !defined {
		sec.labels[name] = -1
	}
}

func (sec *section) addLabel(name string) {
	if sec.labels == nil {
		sec.labels = make(map[string]int)
	}
	sec.labels[name] = len(sec.toks)
}

func (sec *section) genLabel(name string) string {
	if _, defined := sec.labels[name]; name[0] == '.' || defined {
		n, tmp := 1, fmt.Sprintf("%s.1", name)
		for _, defined := sec.labels[tmp]; defined; _, defined = sec.labels[tmp] {
			n, tmp = n+1, fmt.Sprintf("%s.%d", name, n)
		}
	}
	return name
}

func (sec *section) renameLabel(old, new string) {
	if n, defined := sec.labels[old]; defined {
		delete(sec.labels, old)
		sec.labels[new] = n
	}
	for i := range sec.refs {
		if sec.refs[i].targName == old {
			sec.refs[i].targName = new
		}
	}
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
	tok.str = targetName
	asm.opts.addRef(tok, targetName, off)
}

func (asm *assembler) genProgLabel(name string) string {
	name = asm.prog.genLabel(name)
	asm.prog.addLabel(name)
	asm.addAddrLabel(name)
	return name
}

func (asm *assembler) renameLabel(old, new string) string {
	new = asm.prog.genLabel(new)
	asm.adls.renameLabel(old, new)
	asm.opts.renameLabel(old, new)
	asm.prog.renameLabel(old, new)
	for i, tok := range asm.adls.toks {
		if tok.kind == addrLabelTK && tok.str == old {
			oldSize := tok.NeededSize()
			tok.str = new
			asm.adls.toks[i] = tok
			newSize := tok.NeededSize()
			asm.adls.maxBytes += newSize - oldSize
			break
		}
	}
	return new
}

func (asm *assembler) addAddrLabel(name string) {
	if len(asm.adls.toks) == 0 {
		asm.adls.add(optToken("addrLabels", 1, true))
	} else {
		asm.adls.toks[0].Arg++
	}
	asm.adls.addRef(addrLabelToken(name), name, 0)
}

func (asm *assembler) scan(in []interface{}) error {
	sc := scanner{assembler: asm}
	return sc.scan(in)
}

func (asm *assembler) finish() (encoder, error) {
	enc := encoder{
		section: collectSections(
			makeSection(
				optToken("version", 0, false),
			),
			asm.adls,
			asm.opts,
			makeSection(
				optToken("end", 0, false),
			),
			asm.prog,
		),
		logf: asm.logf,
		base: asm.stackSize.Arg,
	}
	err := enc.checkLabels()
	if err == nil {
		enc.refs = enc.resolveRefs()
	}
	return enc, err
}

type scanner struct {
	*assembler
	prior []scannerState
	scannerState
}

type scannerState struct {
	i      int
	in     []interface{}
	state  assemblerState
	label  string
	open   []string
	labels []string
}

func (sc *scanner) scan(in []interface{}) error {
	sc.scannerState = scannerState{
		i:     0,
		in:    in,
		state: assemblerText,
	}
	for {
		for ; sc.i < len(sc.in); sc.i++ {
			sc.label = ""
			if err := sc.handle(sc.in[sc.i]); err != nil {
				return err
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

func (sc *scanner) handle(val interface{}) error {
	switch sc.state {
	case assemblerData:
		return sc.handleData(val)
	case assemblerText:
		return sc.handleText(val)
	default:
		return fmt.Errorf("invalid assembler state %d", sc.state)
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
			return sc.handleDataDirective(v[1:])

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

func (sc *scanner) handleDataDirective(s string) error {
	switch s {
	case "alloc":
		return sc.handleAlloc()
	case "in":
		return sc.handleInput()
	case "out":
		return sc.handleOutput()
	default:
		return sc.handleDirective(s)
	}
}

func (sc *scanner) handleText(val interface{}) error {
	switch v := val.(type) {
	case string:
		switch {
		case len(v) > 1 && v[0] == '.':
			return sc.handleTextDirective(v[1:])

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

func (sc *scanner) handleTextDirective(s string) error {
	switch s {
	default:
		return sc.handleDirective(s)
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
	if err := sc.setState(assemblerText); err != nil {
		return err
	}

	name, err := sc.expectLabel(".entry")
	if err != nil {
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

	return nil
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

	if _, isOpen := sc.opens[name]; isOpen {
		sc.open = append(sc.open, name)
		sc.labels = sc.labels[:0]
	} else {
		sc.labels = append(sc.labels, name)
	}

	sc.addAddrLabel(name)
	sc.prog.addLabel(name)
	sc.label = name
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
	sc.addProgTok(stringToken(sc.pendIn))
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
	sc.addProgTok(stringToken(sc.pendOut))
	sc.pendOut = ""
	return nil
}

func (sc *scanner) handleRef(name string) error {
	tok, err := sc.expectRefOp(0, true, name)
	if err != nil {
		return err
	}
	sc.addProgRef(tok, name, 0)
	sc.refLabel(name)
	return nil
}

func (sc *scanner) handleOffRef(name string, n int) error {
	tok, err := sc.expectRefOp(0, true, name)
	if err != nil {
		return err
	}
	sc.addProgRef(tok, name, n)
	sc.refLabel(name)
	return nil
}

func (sc *scanner) handleOp(name string) error {
	op, err := stackvm.ResolveOp(name, 0, false)
	if err != nil {
		return err
	}
	sc.addProgTok(opToken(op))
	return nil
}

func (sc *scanner) addProgTok(tok token) {
	if tok.kind == opTK && tok.Name() == "ret" {
		if i := len(sc.open) - 1; i >= 0 {
			sc.addSpanClose(sc.genProgLabel(".ret." + sc.open[i]))
			sc.open = sc.open[:i]
		} else if len(sc.labels) > 0 {
			name := sc.genProgLabel(".ret.unknown")
			sc.addSpanClose(name)
			ur := unkRet{
				label:  name,
				cur:    -1,
				labels: sc.labels,
			}
			for _, label := range sc.labels {
				if sc.unkRets == nil {
					sc.unkRets = make(map[string]*unkRet, 1)
				}
				sc.unkRets[label] = &ur
			}
		}
	}
	sc.prog.add(tok)
}

func (sc *scanner) addProgRef(tok token, name string, off int) {
	if tok.kind == opTK && tok.Name() == "call" {
		sc.addSpanOpen(name)
	}
	sc.prog.addRef(tok, name, off)
}

func (sc *scanner) addSpanOpen(name string) {
	if _, isOpen := sc.opens[name]; !isOpen {
		if sc.opens == nil {
			sc.opens = make(map[string]struct{}, 1)
		}
		sc.opens[name] = struct{}{}
		sc.addRefOpt("spanOpen", name, 0)
		if ur := sc.unkRets[name]; ur != nil {
			index := -1
			for i, label := range ur.labels {
				if label == name {
					index = i
				}
			}
			if index > ur.cur {
				new := sc.renameLabel(ur.label, sc.genProgLabel(".ret."+name))
				ur.label = new
				ur.cur = index
			}
		}
	}
}

func (sc *scanner) addSpanClose(name string) {
	sc.addRefOpt("spanClose", name, 0)
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
	sc.addProgTok(opToken(op))
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
	sc.addProgTok(allocToken(uint32(n)))
	return nil
}

func (sc *scanner) handleInput() error {
	name, err := sc.expectLabel(".in")
	if err != nil {
		return err
	}
	sc.pendIn = name // to be flushed by handleLabel
	return nil
}

func (sc *scanner) handleOutput() error {
	name, err := sc.expectLabel(".out")
	if err != nil {
		return err
	}
	sc.pendOut = name // to be flushed by handleLabel
	return nil
}

func (sc *scanner) handleDataWord(d uint32) error {
	sc.addProgTok(dataToken(d))
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
	tok := opToken(op)
	tok.str = name
	return tok, nil
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

func (sc *scanner) expectLabel(role string) (string, error) {
	desc := fmt.Sprintf(`%s "label:"`, role)
	val, err := sc.expect(desc)
	if err != nil {
		return "", err
	}
	if err := sc.handle(val); err != nil {
		return "", err
	}
	if sc.label == "" {
		return "", fmt.Errorf("no label for %s", desc)
	}
	return sc.label, nil
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

	buf     []byte
	offsets []uint32
	c       uint32 // current token offset
	i       int    // current token index
}

func (enc encoder) encode() ([]byte, error) {
	enc.buf = make([]byte, enc.maxBytes)
	enc.offsets = make([]uint32, len(enc.toks)+1)

	var (
		boff  uint32           // offset of encoded program
		nopts int              // count of option tokens
		rfi   int              // index of next ref
		rf    = ref{-1, -1, 0} // next ref
	)

	if len(enc.refs) > 0 {
		rf = enc.refs[rfi]
	}

	// encode options
encodeOptions:
	nopts = 0
	for enc.i < len(enc.toks) {
		if tok, err := enc.encodeTok(); err != nil {
			return nil, err
		} else if tok.kind == optTK && tok.Code == optCodeEnd {
			break
		}
	}
	nopts, boff = enc.i, enc.c

	// encode program
	for enc.i < len(enc.toks) {
		// fix a previously encoded ref's target
		for 0 <= rf.site && rf.site < enc.i && rf.targ <= enc.i {
			// re-encode the ref and rewind if arg size changed
			lo, hi := enc.offsets[rf.site], enc.offsets[rf.site+1]
			site := enc.base + enc.offsets[rf.site] - boff
			targ := enc.base + enc.offsets[rf.targ] - boff + uint32(enc.refs[rfi].off)
			tok := enc.toks[rf.site]
			tok = tok.ResolveRefArg(site, targ)
			enc.toks[rf.site] = tok
			p := enc.buf[lo:]
			n := tok.EncodeInto(p)
			if n <= 0 {
				return nil, fmt.Errorf("failed to recode toks[%d]=%v", rf.site, tok)
			}
			p = p[:n]
			if end := lo + uint32(n); end != hi {
				// rewind to prior ref
				enc.i, enc.c = rf.site+1, end
				enc.offsets[enc.i] = enc.c
				for rfi, rf = range enc.refs {
					if rf.site >= enc.i || rf.targ >= enc.i {
						break
					}
				}
				if enc.i < nopts {
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

		if _, err := enc.encodeTok(); err != nil {
			return nil, err
		}
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

	return enc.buf[:enc.c], nil
}

func (enc *encoder) encodeTok() (token, error) {
	tok := enc.toks[enc.i]
	p := enc.buf[enc.c:]
	if len(p) == 0 {
		return tok, fmt.Errorf("no space to encode toks[%d]=%v", enc.i, tok)
	}
	n := tok.EncodeInto(p)
	if n <= 0 {
		return tok, fmt.Errorf("failed to encode toks[%d]=%v", enc.i, tok)
	}
	enc.c += uint32(n)
	enc.i++
	enc.offsets[enc.i] = enc.c
	return tok, nil
}
