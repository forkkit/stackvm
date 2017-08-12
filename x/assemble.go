package xstackvm

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

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
	return assemble(opts, in[1:])
}

// copied from generated op_codes.go, which isn't that bad since having "the
// zero op crash" should perhaps be the most stable part of the ISA.
const opCodeCrash = 0x00

// dataOp returns an invalid Op that carries a dataToken. These invalid ops are
// used temporarily within within assemble. The ops are "invalid" because thy
// are a "crash with immediate", which will never be represented by a valid
// combination of immToken and opToken.
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

func assemble(opts stackvm.MachOptions, in []interface{}) ([]byte, error) {
	var (
		ops       []stackvm.Op
		jumps     []int
		numJumps  = 0
		maxBytes  = opts.NeededSize()
		labels    = make(map[string]int)
		refs      = make(map[string][]int)
		arg, have = uint32(0), false
		tokz      = tokenizer{
			in:    in,
			out:   make([]token, 0, len(in)),
			state: tokenizerText,
		}
	)

	if err := tokz.scan(); err != nil {
		return nil, err
	}

	for i := 0; i < len(tokz.out); i++ {
		tok := tokz.out[i]

		switch tok.t {
		case labelToken:
			labels[tok.s] = len(ops)

		case refToken:
			ref := tok.s
			// resolve label references
			i++
			tok = tokz.out[i]
			if tok.t != opToken {
				return nil, fmt.Errorf("next token must be an op, got %v instead", tok.t)
			}
			op, err := stackvm.ResolveOp(tok.s, 0, true)
			if err != nil {
				return nil, err
			}
			if !op.AcceptsRef() {
				return nil, fmt.Errorf("%v does not accept ref %q", op, ref)
			}
			maxBytes += 6
			refs[ref] = append(refs[ref], len(ops))
			ops = append(ops, op)
			numJumps++

		case dataToken:
			maxBytes += 4
			ops = append(ops, dataOp(tok.d))

		case immToken:
			// op with immediate arg
			arg, have = tok.d, true
			i++
			tok = tokz.out[i]
			switch tok.t {
			case opToken:
				goto resolveOp
			default:
				return nil, fmt.Errorf("next token must be an op, got %v instead", tok.t)
			}

		case opToken:
			// op without immediate arg
			goto resolveOp

		default:
			return nil, fmt.Errorf("unexpected %v token", tok.t)
		}
		continue

	resolveOp:
		op, err := stackvm.ResolveOp(tok.s, arg, have)
		if err != nil {
			return nil, err
		}
		maxBytes += op.NeededSize()
		ops = append(ops, op)
		arg, have = uint32(0), false

	}

	if numJumps > 0 {
		jumps = make([]int, 0, numJumps)
		for name, sites := range refs {
			i, ok := labels[name]
			if !ok {
				return nil, fmt.Errorf("undefined label %q", name)
			}
			for _, j := range sites {
				ops[j].Arg = uint32(i - j - 1)
			}
			jumps = append(jumps, sites...)
		}
	}

	// setup jump tracking state
	jc := makeJumpCursor(ops, jumps)

	buf := make([]byte, maxBytes)

	n := opts.EncodeInto(buf)
	p := buf[n:]
	base := uint32(opts.StackSize)
	offsets := make([]uint32, len(ops)+1)
	c, i := uint32(0), 0 // current op offset and index
	for i < len(ops) {
		// fix a previously encoded jump's target
		for 0 <= jc.ji && jc.ji < i && jc.ti <= i {
			jIP := base + offsets[jc.ji]
			tIP := base
			if jc.ti < i {
				tIP += offsets[jc.ti]
			} else { // jc.ti == i
				tIP += c
			}
			ops[jc.ji] = ops[jc.ji].ResolveRefArg(jIP, tIP)
			// re-encode the jump and rewind if arg size changed
			lo, hi := offsets[jc.ji], offsets[jc.ji+1]
			if end := lo + uint32(ops[jc.ji].EncodeInto(p[lo:])); end != hi {
				i, c = jc.ji+1, end
				offsets[i] = c
				jc = jc.rewind(i)
			} else {
				jc = jc.next()
			}
		}

		if d, ok := opData(ops[i]); ok {
			// encode a dataToken
			stackvm.ByteOrder.PutUint32(p[c:], d)
			c += 4
			i++
			offsets[i] = c
			continue
		}

		// encode next operation
		c += uint32(ops[i].EncodeInto(p[c:]))
		i++
		offsets[i] = c
	}
	n += int(c)
	buf = buf[:n]

	return buf, nil
}

type tokenType uint8

const (
	labelToken tokenType = iota + 1
	refToken
	opToken
	immToken
	dataToken
)

func (tt tokenType) String() string {
	switch tt {
	case labelToken:
		return "label"
	case refToken:
		return "ref"
	case opToken:
		return "op"
	case immToken:
		return "imm"
	case dataToken:
		return "data"
	default:
		return fmt.Sprintf("InvalidTokenType(%d)", tt)
	}
}

type token struct {
	t tokenType
	s string
	d uint32
}

func label(s string) token  { return token{t: labelToken, s: s} }
func ref(s string) token    { return token{t: refToken, s: s} }
func opName(s string) token { return token{t: opToken, s: s} }
func imm(n int) token       { return token{t: immToken, d: uint32(n)} }
func data(d uint32) token   { return token{t: dataToken, d: d} }

func (t token) String() string {
	switch t.t {
	case labelToken:
		return t.s + ":"
	case refToken:
		return ":" + t.s
	case opToken:
		return t.s
	case immToken:
		return strconv.Itoa(int(t.d))
	default:
		return fmt.Sprintf("InvalidToken(t:%d, s:%q, d:%v)", t.t, t.s, t.d)
	}
}

type tokenizer struct {
	i     int
	in    []interface{}
	out   []token
	state tokenizerState
}

type tokenizerState uint8

const (
	tokenizerText tokenizerState = iota + 1
	tokenizerData
)

func (tokz *tokenizer) scan() error {
	var err error
	for ; err == nil && tokz.i < len(tokz.in); tokz.i++ {
		switch tokz.state {
		case tokenizerData:
			err = tokz.handleData(tokz.in[tokz.i])
		case tokenizerText:
			err = tokz.handleText(tokz.in[tokz.i])
		default:
			return fmt.Errorf("invalid tokenizer state %d", tokz.state)
		}
	}
	return err
}

func (tokz *tokenizer) handleData(val interface{}) error {
	switch v := val.(type) {
	case string:
		switch {
		// directive
		case len(v) > 1 && v[0] == '.':
			return tokz.handleDirective(v)

		// label
		case len(v) > 1 && v[len(v)-1] == ':':
			tokz.out = append(tokz.out, label(v[:len(v)-1]))
			return nil

		default:
			return fmt.Errorf("unexpected string %q", v)
		}

	// data word
	case int:
		tokz.out = append(tokz.out, data(uint32(v)))
		return nil

	default:
		return fmt.Errorf(`invalid token %T(%v); expected ".directive", "label:", or an int`, val, val)
	}
}

func (tokz *tokenizer) handleText(val interface{}) error {
	switch v := val.(type) {
	case string:
		switch {
		// directive
		case len(v) > 1 && v[0] == '.':
			return tokz.handleDirective(v)

		// label
		case len(v) > 1 && v[len(v)-1] == ':':
			tokz.out = append(tokz.out, label(v[:len(v)-1]))
			return nil

		// ref
		case len(v) > 1 && v[0] == ':':
			tokz.out = append(tokz.out, ref(v[1:]))
			return tokz.expectOp()

		// opName
		default:
			tokz.out = append(tokz.out, opName(v))
			return nil
		}

	// imm
	case int:
		tokz.out = append(tokz.out, imm(v))
		return tokz.expectOp()

	default:
		return fmt.Errorf(`invalid token %T(%v); expected ".directive", "label:", ":ref", "opName", or an int`, val, val)
	}
}

func (tokz *tokenizer) handleDirective(s string) error {
	switch s[1:] {
	case "data":
		tokz.state = tokenizerData
		return nil
	case "text":
		tokz.state = tokenizerText
		return nil
	default:
		return fmt.Errorf("invalid directive %s", s)
	}
}

func (tokz *tokenizer) expectOp() error {
	s, err := tokz.expectString(`"opName"`)
	if err == nil {
		tokz.out = append(tokz.out, opName(s))
	}
	return err
}

func (tokz *tokenizer) expectString(desc string) (string, error) {
	val, err := tokz.expect(desc)
	if err == nil {
		if s, ok := val.(string); ok {
			return s, nil
		}
		err = fmt.Errorf("invalid token %T(%v); expected %s", val, val, desc)
	}
	return "", err
}

func (tokz *tokenizer) expect(desc string) (interface{}, error) {
	tokz.i++
	if tokz.i < len(tokz.in) {
		return tokz.in[tokz.i], nil
	}
	return nil, fmt.Errorf("unexpected end of input, expected %s", desc)
}

type jumpCursor struct {
	jumps []int // op indices that are jumps
	offs  []int // jump offsets, mined out of op args
	i     int   // index of the current jump in jumps...
	ji    int   // ...op index of its jump
	ti    int   // ...op index of its target
}

func makeJumpCursor(ops []stackvm.Op, jumps []int) jumpCursor {
	jc := jumpCursor{jumps: jumps, ji: -1, ti: -1}
	if len(jumps) > 0 {
		sort.Ints(jumps)
		// TODO: offs only for jumps
		offs := make([]int, len(ops))
		for i := range ops {
			offs[i] = int(int32(ops[i].Arg))
		}
		jc.offs = offs
		jc.ji = jc.jumps[0]
		jc.ti = jc.ji + 1 + jc.offs[jc.ji]
	}
	return jc
}

func (jc jumpCursor) next() jumpCursor {
	jc.i++
	if jc.i >= len(jc.jumps) {
		jc.ji, jc.ti = -1, -1
	} else {
		jc.ji = jc.jumps[jc.i]
		jc.ti = jc.ji + 1 + jc.offs[jc.ji]
	}
	return jc
}

func (jc jumpCursor) rewind(ri int) jumpCursor {
	for i, ji := range jc.jumps {
		ti := ji + 1 + jc.offs[ji]
		if ji >= ri || ti >= ri {
			jc.i, jc.ji, jc.ti = i, ji, ti
			break
		}
	}
	return jc
}
