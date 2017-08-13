package xstackvm

import (
	"errors"
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
	if len(in) < 2 {
		return nil, errors.New("program too short, need at least options and one token")
	}

	// first element is ~ machine options
	var opts stackvm.MachOptions
	switch v := in[0].(type) {
	case int:
		if v < +0 || v > 0xffff {
			return nil, fmt.Errorf("stackSize %d out of range, must be in (0, 65536)", v)
		}
		opts.StackSize = uint16(v)

	case stackvm.MachOptions:
		opts = v

	default:
		return nil, fmt.Errorf("invalid machine options, "+
			"expected a stackvm.MachOptions or an int, "+
			"but got %T(%v) instead",
			v, v)
	}

	// rest is assembly tokens
	asm := assembler{
		opts:  opts,
		in:    in[1:],
		state: tokenizerText,
	}
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
	state    tokenizerState
	opts     stackvm.MachOptions
	ops      []stackvm.Op
	refs     []ref
	maxBytes int
	labels   map[string]int
	refsBy   map[string][]ref
}

type tokenizerState uint8

const (
	tokenizerText tokenizerState = iota + 1
	tokenizerData
)

func (asm *assembler) scan() error {
	asm.maxBytes = asm.opts.NeededSize()
	asm.labels = make(map[string]int)
	asm.refsBy = make(map[string][]ref)
	var err error
	for ; err == nil && asm.i < len(asm.in); asm.i++ {
		switch asm.state {
		case tokenizerData:
			err = asm.handleData(asm.in[asm.i])
		case tokenizerText:
			err = asm.handleText(asm.in[asm.i])
		default:
			return fmt.Errorf("invalid tokenizer state %d", asm.state)
		}
	}
	if err != nil {
		return err
	}
	return asm.buildRefs()
}

func (asm *assembler) handleData(val interface{}) error {
	switch v := val.(type) {
	case string:
		switch {
		case len(v) > 1 && v[0] == '.':
			return asm.handleDirective(v)

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
			return asm.handleDirective(v)

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

func (asm *assembler) handleDirective(s string) error {
	switch s[1:] {
	case "data":
		asm.state = tokenizerData
		return nil
	case "text":
		asm.state = tokenizerText
		return nil
	default:
		return fmt.Errorf("invalid directive %s", s)
	}
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

func (asm *assembler) buildRefs() error {
	n := 0
	for name, refs := range asm.refsBy {
		if _, defined := asm.labels[name]; !defined {
			return fmt.Errorf("undefined label %q", name)
		}
		n += len(refs)
	}
	if n > 0 {
		asm.refs = make([]ref, 0, n)
		for name, refs := range asm.refsBy {
			targ := asm.labels[name]
			for _, rf := range refs {
				rf.targ = targ
				asm.refs = append(asm.refs, rf)
			}
		}
	}
	return nil
}

func (asm *assembler) encode() []byte {
	// setup ref tracking state
	refs := asm.refs
	rfi, rf := 0, ref{site: -1, targ: -1}
	if len(refs) > 0 {
		sort.Slice(refs, func(i, j int) bool {
			return refs[i].site < refs[j].site
		})
		rf = refs[rfi]
	}

	buf := make([]byte, asm.maxBytes)

	n := asm.opts.EncodeInto(buf)
	p := buf[n:]
	base := uint32(asm.opts.StackSize)
	offsets := make([]uint32, len(asm.ops)+1)
	c, i := uint32(0), 0 // current op offset and index
	for i < len(asm.ops) {
		// fix a previously encoded ref's target
		for 0 <= rf.site && rf.site < i && rf.targ <= i {
			op := asm.ops[rf.site].ResolveRefArg(
				base+offsets[rf.site],
				base+offsets[rf.targ]+uint32(refs[rfi].off))
			asm.ops[rf.site] = op
			// re-encode the ref and rewind if arg size changed
			lo, hi := offsets[rf.site], offsets[rf.site+1]
			if end := lo + uint32(op.EncodeInto(p[lo:])); end != hi {
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
			stackvm.ByteOrder.PutUint32(p[c:], d)
			c += 4
			i++
			offsets[i] = c
			continue
		}

		// encode next operation
		c += uint32(op.EncodeInto(p[c:]))
		i++
		offsets[i] = c
	}
	n += int(c)
	buf = buf[:n]

	return buf
}
