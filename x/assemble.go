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
	asm := assembler{in: in}
	if err := asm.scan(); err != nil {
		return nil, err
	}
	return asm.encode(), nil
}

// copied from generated op_codes.go, which isn't that bad since having "the
// zero op crash" should perhaps be the most stable part of the ISA.
const opCodeCrash = 0x00

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
	i        int
	in       []interface{}
	state    assemblerState
	opts     stackvm.MachOptions
	ops      []stackvm.Op
	maxBytes int
	labels   map[string]int
	refsBy   map[string][]ref
}

type assemblerState uint8

const (
	assemblerText assemblerState = iota + 1
	assemblerData
)

const defaultStackSize = 0x40

func (asm *assembler) init() error {
	if asm.opts.StackSize == 0 {
		asm.opts.StackSize = defaultStackSize
	}
	asm.i = 0
	// TODO in
	asm.state = assemblerText
	asm.ops = nil
	asm.maxBytes = 0
	asm.labels = make(map[string]int)
	asm.refsBy = make(map[string][]ref)
	op, err := stackvm.ResolveOp("jump", 0, true)
	if err != nil {
		return err
	}
	if err == nil {
		asm.ops = append(asm.ops, op)
		asm.maxBytes += 6
	}
	return nil
}

func (asm *assembler) scan() error {
	err := asm.init()
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
	asm.maxBytes += asm.opts.NeededSize()

	// check for undefined labels
	if err == nil {
		var undefined []string
		for name := range asm.refsBy {
			if _, defined := asm.labels[name]; !defined {
				undefined = append(undefined, name)
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
	asm.opts.QueueSize = uint32(n)
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
	asm.opts.MaxOps = uint32(n)
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
	asm.opts.MaxCopies = uint32(n)
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
	asm.opts.StackSize = uint16(n)
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
	asm.labels[".entry"] = len(asm.ops)

	// back-fill the ref for the jump in ops[0]
	asm.refsBy[name] = append(asm.refsBy[name], ref{site: 0})

	asm.setState(assemblerText)
	return nil
}

func (asm *assembler) handleLabel(name string) error {
	if i, defined := asm.labels[name]; defined && i >= 0 {
		return fmt.Errorf("label %q already defined", name)
	}
	asm.labels[name] = len(asm.ops)
	return nil
}

func (asm *assembler) handleRef(name string) error {
	op, err := asm.expectRefOp(0, true, name)
	if err != nil {
		return err
	}
	asm.maxBytes += 6
	asm.defRef(name, 0)
	asm.ops = append(asm.ops, op)
	return nil
}

func (asm *assembler) handleOffRef(name string, n int) error {
	op, err := asm.expectRefOp(0, true, name)
	if err != nil {
		return err
	}
	asm.maxBytes += 6
	asm.defRef(name, n)
	asm.ops = append(asm.ops, op)
	return nil
}

func (asm *assembler) handleOp(name string) error {
	op, err := stackvm.ResolveOp(name, 0, false)
	if err == nil {
		asm.maxBytes += op.NeededSize()
		asm.ops = append(asm.ops, op)
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
		asm.maxBytes += op.NeededSize()
		asm.ops = append(asm.ops, op)
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
	asm.maxBytes += 4 * n
	for i := 0; i < n; i++ {
		asm.ops = append(asm.ops, do)
	}
	return nil
}

func (asm *assembler) handleDataWord(d uint32) error {
	asm.maxBytes += 4
	asm.ops = append(asm.ops, dataOp(d))
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

func (asm *assembler) defRef(name string, off int) {
	rf := ref{site: len(asm.ops), off: off}
	asm.refsBy[name] = append(asm.refsBy[name], rf)
	if _, defined := asm.labels[name]; !defined {
		asm.labels[name] = -len(asm.ops) - 1
	}
}

func (asm *assembler) encode() []byte {
	var (
		refs []ref
	)

	numRefs := 0
	for _, rfs := range asm.refsBy {
		numRefs += len(rfs)
	}
	if numRefs > 0 {
		refs = make([]ref, 0, numRefs)
		for name, rfs := range asm.refsBy {
			targ := asm.labels[name]
			for _, rf := range rfs {
				rf.targ = targ
				refs = append(refs, rf)
			}
		}
	}

	buf := make([]byte, asm.maxBytes)
	base := uint32(asm.opts.StackSize)
	offsets := make([]uint32, len(asm.ops)+1)

	var (
		c   uint32 // current op offset
		i   int    // current op index
		n   uint32 // length of actual encoded
		rfi int
		rf  = ref{site: -1, targ: -1}
	)

	// setup ref tracking state
	if len(refs) > 0 {
		sort.Slice(refs, func(i, j int) bool {
			return refs[i].site < refs[j].site
		})
		rf = refs[rfi]
	}

	// encode options
	n += uint32(asm.opts.EncodeInto(buf))

	// encode program
	if _, defined := asm.labels[".entry"]; !defined {
		// skip unused entry jump
		i++
	}
	for i < len(asm.ops) {
		// fix a previously encoded ref's target
		for 0 <= rf.site && rf.site < i && rf.targ <= i {
			site := base + offsets[rf.site]
			targ := base + offsets[rf.targ] + uint32(refs[rfi].off)
			op := asm.ops[rf.site].ResolveRefArg(site, targ)
			asm.ops[rf.site] = op
			// re-encode the ref and rewind if arg size changed
			lo, hi := offsets[rf.site], offsets[rf.site+1]
			if end := lo + uint32(op.EncodeInto(buf[n+lo:])); end != hi {
				// rewind to prior ref
				i, c = rf.site+1, end
				offsets[i] = c
				for rfi, rf = range refs {
					if rf.site >= i || rf.targ >= i {
						break
					}
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

		op := asm.ops[i]
		if d, ok := opData(op); ok {
			// encode a data word
			stackvm.ByteOrder.PutUint32(buf[n+c:], d)
			c += 4
			i++
			offsets[i] = c
			continue
		}

		// encode next operation
		c += uint32(op.EncodeInto(buf[n+c:]))
		i++
		offsets[i] = c
	}
	n += c

	return buf[:n]
}
