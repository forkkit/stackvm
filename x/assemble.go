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

// NewAssembler creates a new Assembler.
func NewAssembler() Assembler {
	asm := assembler{}
	return asm
}

// Assembler will assemble a stream of generic tokens into machine code in a
// byte slice.
type Assembler interface {
	Assemble(in ...interface{}) ([]byte, error)
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
	i      int
	in     []interface{}
	state  assemblerState
	labels map[string]int

	opts, prog section

	stackSize *stackvm.Op
	queueSize *stackvm.Op
	maxOps    *stackvm.Op
	maxCopies *stackvm.Op
}

func (asm assembler) Assemble(in ...interface{}) (buf []byte, err error) {
	asm.labels = make(map[string]int)
	asm.opts = makeSection()
	asm.prog = makeSection()

	asm.addOpt("version", 0, false)
	asm.stackSize = asm.refOpt("stackSize", defaultStackSize, true)

	var op stackvm.Op
	op, err = stackvm.ResolveOp("jump", 0, true)
	if err != nil {
		return
	}
	asm.prog.ops = append(asm.prog.ops, op)
	asm.prog.maxBytes += 6

	asm.i = 0
	asm.in = in
	asm.state = assemblerText
	err = asm.scan()

	if err == nil {
		buf = asm.encode()
	}

	return
}

func collectSections(
	labels map[string]int, secs ...section,
) (
	refs []ref, ops []stackvm.Op, maxBytes int,
) {
	numRefs, numOps := 0, 0
	for _, sec := range secs {
		numOps += len(sec.ops)
		for _, rfs := range sec.refsBy {
			numRefs += len(rfs)
		}
		maxBytes += sec.maxBytes
	}
	if numOps > 0 {
		ops = make([]stackvm.Op, 0, numOps)
	}
	if numRefs > 0 {
		refs = make([]ref, 0, numRefs)
	}

	for _, sec := range secs {
		base := len(ops)

		// collect ops
		ops = append(ops, sec.ops...)

		// collect refs
		for name, rfs := range sec.refsBy {
			targ := labels[name]
			for _, rf := range rfs {
				rf.site += base
				rf.targ = targ + base
				refs = append(refs, rf)
			}
		}
	}

	if len(refs) > 0 {
		sort.Slice(refs, func(i, j int) bool {
			return refs[i].site < refs[j].site
		})
	}

	return
}

type section struct {
	ops      []stackvm.Op
	refsBy   map[string][]ref
	maxBytes int
}

func makeSection() section {
	return section{
		ops:      nil,
		refsBy:   make(map[string][]ref),
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

func (asm *assembler) scan() error {
	var err error
	for ; err == nil && asm.i < len(asm.in); asm.i++ {
		switch asm.state {
		case assemblerData:
			err = asm.handleData(asm.in[asm.i])
		case assemblerText:
			err = asm.handleText(asm.in[asm.i])
		default:
			return fmt.Errorf("invalid assembler state %d", asm.state)
		}
	}

	// finish options
	asm.addOpt("end", 0, false)

	// check for undefined labels
	if err == nil {
		var undefined []string
		for _, sec := range []section{asm.opts, asm.prog} {
			for name := range sec.refsBy {
				if i, defined := asm.labels[name]; !defined || i < 0 {
					undefined = append(undefined, name)
				}
			}
		}
		if len(undefined) > 0 {
			err = fmt.Errorf("undefined labels: %q", undefined)
		}
	}

	return err
}

func (asm *assembler) handleQueueSize() error {
	n, err := asm.expectInt("queueSize int")
	if err != nil {
		return err
	}
	if n < 0 {
		return fmt.Errorf("invalid .queueSize %v, must be non-negative", n)
	}
	if asm.queueSize == nil {
		asm.queueSize = asm.refOpt("queueSize", uint32(n), true)
	} else {
		asm.queueSize.Arg = uint32(n)
	}
	return nil
}

func (asm *assembler) handleMaxOps() error {
	n, err := asm.expectInt("maxOps int")
	if err != nil {
		return err
	}
	if n < 0 {
		return fmt.Errorf("invalid .maxOps %v, must be non-negative", n)
	}
	if asm.maxOps == nil {
		asm.maxOps = asm.refOpt("maxOps", uint32(n), true)
	} else {
		asm.maxOps.Arg = uint32(n)
	}
	return nil
}

func (asm *assembler) handleMaxCopies() error {
	n, err := asm.expectInt("maxCopies int")
	if err != nil {
		return err
	}
	if n < 0 {
		return fmt.Errorf("invalid .maxCopies %v, must be non-negative", n)
	}
	if asm.maxCopies == nil {
		asm.maxCopies = asm.refOpt("maxCopies", uint32(n), true)
	} else {
		asm.maxCopies.Arg = uint32(n)
	}
	return nil
}

func (asm *assembler) handleStackSize() error {
	n, err := asm.expectInt("stackSize int")
	if err != nil {
		return err
	}
	if n < +0 || n > 0xffff {
		return fmt.Errorf("stackSize %d out of range, must be in (0x0000, 0xffff)", n)
	}
	asm.stackSize.Arg = uint32(n)
	return nil
}

func (asm *assembler) handleData(val interface{}) error {
	switch v := val.(type) {
	case string:
		switch {
		case len(v) > 1 && v[0] == '.':
			switch s := v[1:]; s {
			case "alloc":
				return asm.handleAlloc()
			default:
				return asm.handleDirective(s)
			}

		case len(v) > 1 && v[len(v)-1] == ':':
			return asm.handleLabel(v[:len(v)-1])

		default:
			return fmt.Errorf("unexpected string %q", v)
		}

	case int:
		return asm.handleDataWord(uint32(v))

	default:
		return fmt.Errorf(`invalid token %T(%v); expected ".directive", "label:", or an int`, val, val)
	}
}

func (asm *assembler) handleText(val interface{}) error {
	switch v := val.(type) {
	case string:
		switch {
		case len(v) > 1 && v[0] == '.':
			return asm.handleDirective(v[1:])

		case len(v) > 1 && v[len(v)-1] == ':':
			return asm.handleLabel(v[:len(v)-1])

		case len(v) > 1 && v[0] == ':':
			return asm.handleRef(v[1:])

		default:
			return asm.handleOp(v)
		}

	case int:
		return asm.handleImm(v)

	default:
		return fmt.Errorf(`invalid token %T(%v); expected ".directive", "label:", ":ref", "opName", or an int`, val, val)
	}
}

func (asm *assembler) handleDirective(name string) error {
	switch name {
	case "entry":
		return asm.handleEntry()
	case "stackSize":
		return asm.handleStackSize()
	case "queueSize":
		return asm.handleQueueSize()
	case "maxOps":
		return asm.handleMaxOps()
	case "maxCopies":
		return asm.handleMaxCopies()
	case "data":
		asm.setState(assemblerData)
		return nil
	case "text":
		asm.setState(assemblerText)
		return nil
	default:
		return fmt.Errorf("invalid directive .%s", name)
	}
}

func (asm *assembler) setState(state assemblerState) {
	asm.state = state
}

func (asm *assembler) handleEntry() error {
	s, err := asm.expectString(`"label:"`)
	if err != nil {
		return err
	}

	// expect and define label
	if len(s) < 2 || s[len(s)-1] != ':' {
		return fmt.Errorf("unexpected string %q, expected .entry label", s)
	}
	name := s[:len(s)-1]
	if err := asm.handleLabel(name); err != nil {
		return err
	}

	// dupe check .entry
	if i, defined := asm.labels[".entry"]; defined && i >= 0 {
		for dupName, j := range asm.labels {
			if j == i {
				return fmt.Errorf("duplicate .entry %q, already set to %q", name, dupName)
			}
		}
		return fmt.Errorf("duplicate .entry %q, already set to ???", name)
	}
	asm.labels[".entry"] = len(asm.prog.ops)

	// back-fill the ref for the jump in ops[0]
	asm.prog.refsBy[name] = append(asm.prog.refsBy[name], ref{site: 0})

	asm.setState(assemblerText)
	return nil
}

func (asm *assembler) handleLabel(name string) error {
	if i, defined := asm.labels[name]; defined && i >= 0 {
		return fmt.Errorf("label %q already defined", name)
	}
	asm.labels[name] = len(asm.prog.ops)
	return nil
}

func (asm *assembler) handleRef(name string) error {
	op, err := asm.expectRefOp(0, true, name)
	if err != nil {
		return err
	}
	asm.prog.addRef(op, name, 0)
	asm.refLabel(name)
	return nil
}

func (asm *assembler) handleOffRef(name string, n int) error {
	op, err := asm.expectRefOp(0, true, name)
	if err != nil {
		return err
	}
	asm.prog.addRef(op, name, n)
	asm.refLabel(name)
	return nil
}

func (asm *assembler) handleOp(name string) error {
	op, err := stackvm.ResolveOp(name, 0, false)
	if err == nil {
		asm.prog.add(op)
	}
	return err
}

func (asm *assembler) handleImm(n int) (err error) {
	var op stackvm.Op
	s, err := asm.expectString(`":ref" or "opName"`)
	if err == nil {
		if len(s) > 1 && s[0] == ':' {
			return asm.handleOffRef(s[1:], n)
		}
		op, err = stackvm.ResolveOp(s, uint32(n), true)
	}
	if err == nil {
		asm.prog.add(op)
	}
	return err
}

func (asm *assembler) handleAlloc() error {
	n, err := asm.expectInt(`int`)
	if err != nil {
		return err
	}
	if n < 1 {
		return fmt.Errorf("invalid .alloc %v, must be positive", n)
	}
	// TODO: should be in bytes, not words
	// TODO: would like to avoid N*append
	do := dataOp(0)
	asm.prog.maxBytes += 4 * n
	for i := 0; i < n; i++ {
		asm.prog.add(do)
	}
	return nil
}

func (asm *assembler) handleDataWord(d uint32) error {
	asm.prog.maxBytes += 4
	asm.prog.add(dataOp(d))
	return nil
}

func (asm *assembler) expectRefOp(arg uint32, have bool, name string) (op stackvm.Op, err error) {
	opName, err := asm.expectString(`"opName"`)
	if err == nil {
		op, err = stackvm.ResolveOp(opName, arg, have)
	}
	if err == nil && !op.AcceptsRef() {
		err = fmt.Errorf("%v does not accept ref %q", op, name)
	}
	return
}

func (asm *assembler) expectOp(arg uint32, have bool) (op stackvm.Op, err error) {
	opName, err := asm.expectString(`"opName"`)
	if err == nil {
		op, err = stackvm.ResolveOp(opName, arg, have)
	}
	return
}

func (asm *assembler) expectString(desc string) (string, error) {
	val, err := asm.expect(desc)
	if err == nil {
		if s, ok := val.(string); ok {
			return s, nil
		}
		err = fmt.Errorf("invalid token %T(%v); expected %s", val, val, desc)
	}
	return "", err
}

func (asm *assembler) expectInt(desc string) (int, error) {
	val, err := asm.expect(desc)
	if err == nil {
		if n, ok := val.(int); ok {
			return n, nil
		}
		err = fmt.Errorf("invalid token %T(%v); expected %s", val, val, desc)
	}
	return 0, err
}

func (asm *assembler) expect(desc string) (interface{}, error) {
	asm.i++
	if asm.i < len(asm.in) {
		return asm.in[asm.i], nil
	}
	return nil, fmt.Errorf("unexpected end of input, expected %s", desc)
}

func (asm *assembler) refLabel(name string) {
	if _, defined := asm.labels[name]; !defined {
		asm.labels[name] = -1
	}
}

func (asm *assembler) encode() []byte {
	var (
		base  = asm.stackSize.Arg
		boff  uint32 // position of encoded program
		nopts = len(asm.opts.ops)
	)

	refs, ops, maxBytes := collectSections(asm.labels, asm.opts, asm.prog)
	rfi, rf := 0, ref{site: -1, targ: -1}
	if len(refs) > 0 {
		rf = refs[rfi]
	}

	var (
		buf     = make([]byte, maxBytes)
		offsets = make([]uint32, len(ops)+1)
		c       uint32 // current op offset
		i       int    // current op index
	)

	// encode options
encodeOptions:
	for i < len(ops) {
		op := ops[i]
		c += uint32(op.EncodeInto(buf[c:]))
		i++
		offsets[i] = c
		if op.Code == optCodeEnd {
			break
		}
	}
	boff = c

	// encode program
	if _, defined := asm.labels[".entry"]; !defined {
		// skip unused entry jump
		i++
		offsets[i] = c
	}
	for i < len(ops) {
		// fix a previously encoded ref's target
		for 0 <= rf.site && rf.site < i && rf.targ <= i {
			// re-encode the ref and rewind if arg size changed
			lo, hi := offsets[rf.site], offsets[rf.site+1]
			site := base + offsets[rf.site] - boff
			targ := base + offsets[rf.targ] - boff + uint32(refs[rfi].off)
			op := ops[rf.site]
			op = op.ResolveRefArg(site, targ)
			ops[rf.site] = op
			if end := lo + uint32(op.EncodeInto(buf[lo:])); end != hi {
				// rewind to prior ref
				i, c = rf.site+1, end
				offsets[i] = c
				for rfi, rf = range refs {
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
				if rfi >= len(refs) {
					rf = ref{site: -1, targ: -1}
				} else {
					rf = refs[rfi]
				}
			}
		}

		op := ops[i]
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
