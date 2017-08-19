package stackvm

const (
	opCodeCrash   = opCode(0x00)
	opCodeNop     = opCode(0x01)
	opCodePush    = opCode(0x02)
	opCodePop     = opCode(0x03)
	opCodeDup     = opCode(0x04)
	opCodeSwap    = opCode(0x05)
	opCodeFetch   = opCode(0x08)
	opCodeStore   = opCode(0x09)
	opCodeStoreto = opCode(0x0a)
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
	opCodeP2C     = opCode(0x2a)
	opCodeC2P     = opCode(0x2b)
	opCodeMark    = opCode(0x2c)
	opCodeJump    = opCode(0x30)
	opCodeJnz     = opCode(0x31)
	opCodeJz      = opCode(0x32)
	opCodeCall    = opCode(0x33)
	opCodeRet     = opCode(0x34)
	opCodeFork    = opCode(0x40)
	opCodeFnz     = opCode(0x41)
	opCodeFz      = opCode(0x42)
	opCodeBranch  = opCode(0x50)
	opCodeBnz     = opCode(0x51)
	opCodeBz      = opCode(0x52)
	opCodeBitnot  = opCode(0x58)
	opCodeBitand  = opCode(0x59)
	opCodeBitor   = opCode(0x5a)
	opCodeBitxor  = opCode(0x5b)
	opCodeShiftl  = opCode(0x5c)
	opCodeShiftr  = opCode(0x5d)
	opCodeBitest  = opCode(0x60)
	opCodeBitset  = opCode(0x61)
	opCodeBitost  = opCode(0x62)
	opCodeBitseta = opCode(0x63)
	opCodeBitosta = opCode(0x64)
	opCodeHnz     = opCode(0x7d)
	opCodeHz      = opCode(0x7e)
	opCodeHalt    = opCode(0x7f)
)
