package stackvm

type op func(*Mach) error

type opDecoder func(arg uint32, have bool) op

type opCode uint8

const opCodeWithImm = opCode(0x80)

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
	return "InvalidImmediate"
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
	// XXX noop but maybe halt should be 0!
	valop("push"), valop("pop"), valop("dup"), valop("swap"),
	noop, noop, noop, noop,
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
	justop("loop"), justop("lnz"), justop("lz"),
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

const (
	// TODO: codegen this
	opCodePush    = opCode(0x00)
	opCodePop     = opCode(0x01)
	opCodeDup     = opCode(0x02)
	opCodeSwap    = opCode(0x03)
	opCodeFetch   = opCode(0x08)
	opCodeStore   = opCode(0x09)
	opCodeStoreTo = opCode(0x0a)
	opCodeAdd     = opCode(0x10)
	opCodeSub     = opCode(0x11)
	opCodeMul     = opCode(0x12)
	opCodeDiv     = opCode(0x13)
	opCodeMod     = opCode(0x14)
	opCodeDivmod  = opCode(0x15)
	opCodeNeg     = opCode(0x16)
	opCodeLt      = opCode(0x18)
	opCodeLte     = opCode(0x19)
	opCodeGt      = opCode(0x1a)
	opCodeGte     = opCode(0x1b)
	opCodeEq      = opCode(0x1c)
	opCodeNeq     = opCode(0x1d)
	opCodeNot     = opCode(0x20)
	opCodeAnd     = opCode(0x21)
	opCodeOr      = opCode(0x22)
	opCodeCpush   = opCode(0x28)
	opCodeCpop    = opCode(0x29)
	opCodeP2c     = opCode(0x2a)
	opCodeC2p     = opCode(0x2b)
	opCodeMark    = opCode(0x2c)
	opCodeJump    = opCode(0x30)
	opCodeJnz     = opCode(0x31)
	opCodeJz      = opCode(0x32)
	opCodeLoop    = opCode(0x33)
	opCodeLnz     = opCode(0x34)
	opCodeLz      = opCode(0x35)
	opCodeCall    = opCode(0x36)
	opCodeRet     = opCode(0x37)
	opCodeFork    = opCode(0x40)
	opCodeFnz     = opCode(0x41)
	opCodeFz      = opCode(0x42)
	opCodeBranch  = opCode(0x50)
	opCodeBnz     = opCode(0x51)
	opCodeBz      = opCode(0x52)
	opCodeHnz     = opCode(0x7d)
	opCodeHz      = opCode(0x7e)
	opCodeHalt    = opCode(0x7f)
)

var opName2Code = make(map[string]byte, 128)

func init() {
	for i, def := range ops {
		opName2Code[def.name] = byte(i)
	}
}
