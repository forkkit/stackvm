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

type ref struct{ site, targ int }

type assembler struct {
	i        int
	in       []interface{}
	state    tokenizerState
	opts     stackvm.MachOptions
	ops      []stackvm.Op
	refs     []ref
	maxBytes int
	labels   map[string]int
	refSites map[string][]int
}

type tokenizerState uint8

const (
	tokenizerText tokenizerState = iota + 1
	tokenizerData
)

func (asm *assembler) scan() error {
	asm.maxBytes = asm.opts.NeededSize()
	asm.labels = make(map[string]int)
	asm.refSites = make(map[string][]int)

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

	n := 0
	for name, sites := range asm.refSites {
		if _, defined := asm.labels[name]; !defined {
			return fmt.Errorf("undefined label %q", name)
		}
		n += len(sites)
	}
	if n > 0 {
		asm.refs = make([]ref, 0, n)
		for name, sites := range asm.refSites {
			targ := asm.labels[name]
			for _, site := range sites {
				asm.refs = append(asm.refs, ref{site, targ})
			}
		}
	}

	return nil
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
		return asm.handleImm(uint32(v))

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
	op, err := asm.expectOp(0, true)
	if err != nil {
		return err
	}
	if !op.AcceptsRef() {
		return fmt.Errorf("%v does not accept ref %q", op, name)
	}
	asm.maxBytes += 6
	asm.refSites[name] = append(asm.refSites[name], len(asm.ops))
	if _, defined := asm.labels[name]; !defined {
		asm.labels[name] = -len(asm.ops) - 1
	}
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

func (asm *assembler) handleImm(d uint32) error {
	op, err := asm.expectOp(d, true)
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

func (asm *assembler) expect(desc string) (interface{}, error) {
	asm.i++
	if asm.i < len(asm.in) {
		return asm.in[asm.i], nil
	}
	return nil, fmt.Errorf("unexpected end of input, expected %s", desc)
}

func (asm *assembler) encode() []byte {
	// setup ref tracking state
	rc := makeRefCursor(asm.refs)

	buf := make([]byte, asm.maxBytes)

	n := asm.opts.EncodeInto(buf)
	p := buf[n:]
	base := uint32(asm.opts.StackSize)
	offsets := make([]uint32, len(asm.ops)+1)
	c, i := uint32(0), 0 // current op offset and index
	for i < len(asm.ops) {
		// fix a previously encoded ref's target
		for 0 <= rc.si && rc.si < i && rc.ti <= i {
			jIP := base + offsets[rc.si]
			tIP := base + offsets[rc.ti]
			asm.ops[rc.si] = asm.ops[rc.si].ResolveRefArg(jIP, tIP)
			// re-encode the ref and rewind if arg size changed
			lo, hi := offsets[rc.si], offsets[rc.si+1]
			if end := lo + uint32(asm.ops[rc.si].EncodeInto(p[lo:])); end != hi {
				i, c = rc.si+1, end
				offsets[i] = c
				rc = rc.rewind(i)
			} else {
				rc = rc.next()
			}
		}

		if d, ok := opData(asm.ops[i]); ok {
			// encode a data word
			stackvm.ByteOrder.PutUint32(p[c:], d)
			c += 4
			i++
			offsets[i] = c
			continue
		}

		// encode next operation
		c += uint32(asm.ops[i].EncodeInto(p[c:]))
		i++
		offsets[i] = c
	}
	n += int(c)
	buf = buf[:n]

	return buf
}

type refCursor struct {
	refs []ref // ref {site, targ} pairs
	i    int   // index of the current ref in refs...
	si   int   // ...op index of its site
	ti   int   // ...op index of its target
}

func makeRefCursor(refs []ref) refCursor {
	rc := refCursor{si: -1, ti: -1}
	if len(refs) > 0 {
		sort.Slice(refs, func(i, j int) bool {
			return refs[i].site < refs[j].site
		})
		rc.refs = refs
		rc.si = rc.refs[0].site
		rc.ti = rc.refs[0].targ
	}
	return rc
}

func (rc refCursor) next() refCursor {
	rc.i++
	if rc.i >= len(rc.refs) {
		rc.si, rc.ti = -1, -1
	} else {
		rc.si = rc.refs[rc.i].site
		rc.ti = rc.refs[rc.i].targ
	}
	return rc
}

func (rc refCursor) rewind(ri int) refCursor {
	for i, ref := range rc.refs {
		if ref.site >= ri || ref.targ >= ri {
			rc.i, rc.si, rc.ti = i, ref.site, ref.targ
			break
		}
	}
	return rc
}
