package stackvm

import "fmt"

type op func(*Mach) error

type opDecoder func(arg uint32, have bool) op

type opCode uint8

const opCodeWithImm = opCode(0x80)

func (c opCode) String() string {
	od := ops[c.code()]
	name := od.name
	if name == "" {
		name = fmt.Sprintf("UNDEFINED<%#02x>", c.code())
	}
	if c.hasImm() {
		return fmt.Sprintf("%s-%s", od.imm.short(), name)
	}
	return name
}

func (c opCode) hasImm() bool { return (c & opCodeWithImm) != 0 }
func (c opCode) code() uint8  { return uint8(c & ^opCodeWithImm) }

type opImmKind int

const (
	opImmNone = opImmKind(iota)
	opImmVal
	opImmAddr
	opImmOffset

	opImmType  = 0x0f
	opImmFlags = ^0x0f
	opImmReq   = 0x010
)

func (k opImmKind) kind() opImmKind { return k & opImmType }
func (k opImmKind) required() bool  { return (k & opImmReq) != 0 }

func (k opImmKind) short() string {
	switch k {
	case opImmNone:
		return ""
	case opImmVal:
		return "val"
	case opImmAddr:
		return "addr"
	case opImmOffset:
		return "offset"
	}
	return fmt.Sprintf("Invalid<%d>", k)
}

func (k opImmKind) String() string {
	switch k {
	case opImmNone:
		return "NoImmediate"
	case opImmVal:
		return "ImmediateVal"
	case opImmAddr:
		return "ImmediateAddr"
	case opImmOffset:
		return "ImmediateOffset"
	}
	return fmt.Sprintf("InvalidImmediate<%d>", k)
}

type opDef struct {
	name string
	imm  opImmKind
}

var noop = opDef{}

func valop(name string) opDef  { return opDef{name, opImmVal} }
func addrop(name string) opDef { return opDef{name, opImmAddr} }
func offop(name string) opDef  { return opDef{name, opImmOffset} }
func justop(name string) opDef { return opDef{name, opImmNone} }

// TODO: mark required ops
// case opCodePush:
// 	m.err = errImmReq
// case opCodeCpush:
// 	m.err = errImmReq

var ops = [128]opDef{
	// 0x00
	justop("crash"),
	valop("push"), valop("pop"),
	valop("dup"), valop("swap"),
	noop, noop, noop,
	// 0x08
	addrop("fetch"), valop("store"), addrop("storeTo"),
	noop, noop, noop, noop, noop,
	// 0x10
	valop("add"), valop("sub"),
	valop("mul"), valop("div"),
	valop("mod"), valop("divmod"),
	justop("neg"), noop,
	// 0x18
	valop("lt"), valop("lte"), valop("gt"), valop("gte"),
	valop("eq"), valop("neq"), noop, noop,
	// 0x20
	justop("not"), justop("and"), justop("or"),
	noop, noop, noop, noop, noop,
	// 0x28
	valop("cpush"), valop("cpop"), valop("p2c"), valop("c2p"),
	justop("mark"), noop, noop, noop,
	// 0x30
	offop("jump"), offop("jnz"), offop("jz"),
	noop, noop, noop,
	addrop("call"), justop("ret"),
	// 0x38
	noop, noop, noop, noop, noop, noop, noop, noop,
	// 0x40
	offop("fork"), offop("fnz"), offop("fz"),
	noop, noop, noop, noop, noop,
	// 0x48
	noop, noop, noop, noop, noop, noop, noop, noop,
	// 0x50
	offop("branch"), offop("bnz"), offop("bz"),
	noop, noop, noop, noop, noop,
	// 0x58
	noop, noop, noop, noop, noop, noop, noop, noop,
	// 0x60
	noop, noop, noop, noop, noop, noop, noop, noop,
	// 0x68
	noop, noop, noop, noop, noop, noop, noop, noop,
	// 0x70
	noop, noop, noop, noop, noop, noop, noop, noop,
	// 0x78
	noop, noop, noop, noop, noop,
	valop("hnz"), valop("hz"), valop("halt"),
}

//go:generate python gen_op_codes.py -i ops.go -o op_codes.go

var opName2Code = make(map[string]byte, 128)

func init() {
	for i, def := range ops {
		opName2Code[def.name] = byte(i)
	}
}
