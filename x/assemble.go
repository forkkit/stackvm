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

// dataOp returns an invalid Op that carries a data word. These invalid ops are
// used temporarily between assembler.scan and assembler.encode. The ops are
// "invalid" because they are a "crash with immediate", which will never be
// represented by a valid combination of immediate and op name.
func dataOp(d uint32) stackvm.Op {
	return stackvm.Op{
		Code: opCodeCrash,
		Have: true,
		Arg:  d,
	}
}

func opData(op stackvm.Op) (uint32, bool) {
	if op.Code == opCodeCrash && op.Have {
		return op.Arg, true
	}
	return 0, false
}

type ref struct{ site, targ, off int }

type assembler struct {
	logff func(string, ...interface{})

	opts, prog section

	stackSize *stackvm.Op
	queueSize *stackvm.Op
	maxOps    *stackvm.Op
	maxCopies *stackvm.Op
}

func (asm assembler) Assemble(in ...interface{}) ([]byte, error) {
	asm.opts = makeSection()
	asm.prog = makeSection()

	asm.addOpt("version", 0, false)
	asm.stackSize = asm.refOpt("stackSize", defaultStackSize, true)

	if err := asm.scan(in); err != nil {
		return nil, err
	}

	asm.finish()

	enc, err := collectSections(asm.opts, asm.prog)
	if err != nil {
		return nil, err
	}

	enc.logf = asm.logf
	enc.base = asm.stackSize.Arg
	enc.nopts = len(asm.opts.ops)
	return enc.encode(), nil
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

func (asm *assembler) setOption(pop **stackvm.Op, name string, v uint32) {
	if *pop == nil {
		*pop = asm.refOpt(name, v, true)
	} else {
		(*pop).Arg = v
	}
}

func collectSections(secs ...section) (enc encoder, err error) {
	numLabels, numRefs, numOps := 0, 0, 0
	for _, sec := range secs {
		numLabels += len(sec.labels)
		numOps += len(sec.ops)
		for _, rfs := range sec.refsBy {
			numRefs += len(rfs)
		}
		enc.maxBytes += sec.maxBytes
	}
	if numLabels > 0 {
		enc.labels = make(map[string]int)
	}
	if numOps > 0 {
		enc.ops = make([]stackvm.Op, 0, numOps)
	}
	if numRefs > 0 {
		enc.refs = make([]ref, 0, numRefs)
	}

	base := 0
	for _, sec := range secs {
		// collect labels
		for name, off := range sec.labels {
			enc.labels[name] = base + off
		}

		// collect ops
		enc.ops = append(enc.ops, sec.ops...)

		base += len(sec.ops)
	}

	// check for undefined label refs
	var undefined []string
	for _, sec := range secs {
		for name := range sec.refsBy {
			if i, defined := enc.labels[name]; !defined || i < 0 {
				undefined = append(undefined, name)
			}
		}
	}
	if len(undefined) > 0 {
		err = fmt.Errorf("undefined labels: %q", undefined)
		return
	}

	// resolve and collect refs
	base = 0
	for _, sec := range secs {
		for name, rfs := range sec.refsBy {
			targ := enc.labels[name]
			for _, rf := range rfs {
				rf.site += base
				rf.targ = targ
				enc.refs = append(enc.refs, rf)
			}
		}

		base += len(sec.ops)
	}

	if len(enc.refs) > 0 {
		sort.Slice(enc.refs, func(i, j int) bool {
			return enc.refs[i].site < enc.refs[j].site
		})
	}

	return
}

type section struct {
	ops      []stackvm.Op
	refsBy   map[string][]ref
	labels   map[string]int
	maxBytes int
}

func makeSection() section {
	return section{
		ops:      nil,
		refsBy:   make(map[string][]ref),
		labels:   make(map[string]int),
		maxBytes: 0,
	}
}

func (sec *section) add(op stackvm.Op) {
	sec.ops = append(sec.ops, op)
	if _, isData := opData(op); !isData {
		sec.maxBytes += op.NeededSize()
	}
}

func (sec *section) addRef(op stackvm.Op, name string, off int) {
	rf := ref{site: len(sec.ops), off: off}
	sec.refsBy[name] = append(sec.refsBy[name], rf)
	sec.ops = append(sec.ops, op)
	sec.maxBytes += 6
}

type assemblerState uint8

const (
	assemblerText assemblerState = iota + 1
	assemblerData
)

const defaultStackSize = 0x40

func (asm *assembler) refOpt(name string, arg uint32, have bool) *stackvm.Op {
	i := len(asm.opts.ops)
	asm.addOpt(name, arg, have)
	return &asm.opts.ops[i]
}

func (asm *assembler) addOpt(name string, arg uint32, have bool) {
	asm.opts.add(stackvm.ResolveOption(name, arg, have))
}

func (asm *assembler) addRefOpt(name string, targetName string, off int) {
	op := stackvm.ResolveOption(name, 0, true)
	asm.opts.addRef(op, targetName, off)
}

type scanner struct {
	*assembler
	i     int
	in    []interface{}
	state assemblerState
}

func (asm *assembler) scan(in []interface{}) error {
	sc := scanner{
		assembler: asm,
		i:         0,
		in:        in,
		state:     assemblerText,
	}
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
	return nil
}

func (asm *assembler) finish() {
	// finish options
	asm.addOpt("end", 0, false)
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
		sc.setState(assemblerData)
		return nil
	case "text":
		sc.setState(assemblerText)
		return nil
	default:
		return fmt.Errorf("invalid directive .%s", name)
	}
}

func (sc *scanner) setState(state assemblerState) {
	sc.state = state
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
	sc.prog.labels[".entry"] = len(sc.prog.ops)

	sc.addRefOpt("entry", name, 0)

	sc.setState(assemblerText)
	return nil
}

func (sc *scanner) handleLabel(name string) error {
	if i, defined := sc.prog.labels[name]; defined && i >= 0 {
		return fmt.Errorf("label %q already defined", name)
	}
	sc.prog.labels[name] = len(sc.prog.ops)
	return nil
}

func (sc *scanner) handleRef(name string) error {
	op, err := sc.expectRefOp(0, true, name)
	if err != nil {
		return err
	}
	sc.prog.addRef(op, name, 0)
	sc.refLabel(name)
	return nil
}

func (sc *scanner) handleOffRef(name string, n int) error {
	op, err := sc.expectRefOp(0, true, name)
	if err != nil {
		return err
	}
	sc.prog.addRef(op, name, n)
	sc.refLabel(name)
	return nil
}

func (sc *scanner) handleOp(name string) error {
	op, err := stackvm.ResolveOp(name, 0, false)
	if err == nil {
		sc.prog.add(op)
	}
	return err
}

func (sc *scanner) handleImm(n int) (err error) {
	var op stackvm.Op
	s, err := sc.expectString(`":ref" or "opName"`)
	if err == nil {
		if len(s) > 1 && s[0] == ':' {
			return sc.handleOffRef(s[1:], n)
		}
		op, err = stackvm.ResolveOp(s, uint32(n), true)
	}
	if err == nil {
		sc.prog.add(op)
	}
	return err
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
	// TODO: would like to avoid N*append
	do := dataOp(0)
	sc.prog.maxBytes += 4 * n
	for i := 0; i < n; i++ {
		sc.prog.add(do)
	}
	return nil
}

func (sc *scanner) handleDataWord(d uint32) error {
	sc.prog.maxBytes += 4
	sc.prog.add(dataOp(d))
	return nil
}

func (sc *scanner) expectRefOp(arg uint32, have bool, name string) (op stackvm.Op, err error) {
	opName, err := sc.expectString(`"opName"`)
	if err == nil {
		op, err = stackvm.ResolveOp(opName, arg, have)
	}
	if err == nil && !op.AcceptsRef() {
		err = fmt.Errorf("%v does not accept ref %q", op, name)
	}
	return
}

func (sc *scanner) expectOp(arg uint32, have bool) (op stackvm.Op, err error) {
	opName, err := sc.expectString(`"opName"`)
	if err == nil {
		op, err = stackvm.ResolveOp(opName, arg, have)
	}
	return
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
	if _, defined := asm.prog.labels[name]; !defined {
		asm.prog.labels[name] = -1
	}
}

type encoder struct {
	logf     func(string, ...interface{})
	nopts    int
	base     uint32
	labels   map[string]int
	refs     []ref
	ops      []stackvm.Op
	maxBytes int
}

func (enc encoder) encode() []byte {
	var (
		buf     = make([]byte, enc.maxBytes)
		offsets = make([]uint32, len(enc.ops)+1)
		boff    uint32           // offset of encoded program
		c       uint32           // current op offset
		i       int              // current op index
		rfi     int              // index of next ref
		rf      = ref{-1, -1, 0} // next ref
	)

	if len(enc.refs) > 0 {
		rf = enc.refs[rfi]
	}

	// encode options
encodeOptions:
	for i < len(enc.ops) {
		op := enc.ops[i]
		c += uint32(op.EncodeInto(buf[c:]))
		i++
		offsets[i] = c
		if op.Code == optCodeEnd {
			break
		}
	}
	boff = c

	// encode program
	for i < len(enc.ops) {
		// fix a previously encoded ref's target
		for 0 <= rf.site && rf.site < i && rf.targ <= i {
			// re-encode the ref and rewind if arg size changed
			lo, hi := offsets[rf.site], offsets[rf.site+1]
			site := enc.base + offsets[rf.site] - boff
			targ := enc.base + offsets[rf.targ] - boff + uint32(enc.refs[rfi].off)
			op := enc.ops[rf.site]
			if rf.site < enc.nopts {
				op.Arg = targ
			} else {
				op = op.ResolveRefArg(site, targ)
			}
			enc.ops[rf.site] = op
			if end := lo + uint32(op.EncodeInto(buf[lo:])); end != hi {
				// rewind to prior ref
				i, c = rf.site+1, end
				offsets[i] = c
				for rfi, rf = range enc.refs {
					if rf.site >= i || rf.targ >= i {
						break
					}
				}
				if i < enc.nopts {
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

		op := enc.ops[i]
		if d, ok := opData(op); ok {
			// encode a data word
			stackvm.ByteOrder.PutUint32(buf[c:], d)
			c += 4
			i++
			offsets[i] = c
			continue
		}

		// encode next operation
		c += uint32(op.EncodeInto(buf[c:]))
		i++
		offsets[i] = c
	}

	return buf[:c]
}
