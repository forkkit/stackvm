package stackvm_test

import (
	"testing"

	. "github.com/jcorbin/stackvm/x"
)

// Test solving the following cryptarithmetic problem:
//
//         S E N D
//     +   M O R E
//     -----------
//       M O N E Y
//
// For keep the program "simpler", and to sufficiently exercise the vm, a
// simplistic right-to-left or "bottom up" solution strategy is used.
//
// TODO: test solving other solution strategies, including even a more naive
// brute force.
//
// TODO: good way to test any higher level construct, like a forth or even just
// a compiler, would be the case of building the program section for each column.

var smmTest = TestCase{
	Name: "send more money (bottom up)",
	Prog: MustAssemble(
		//     s e n d
		// +   m o r e
		// -----------
		//   m o n e y

		0x40, // stack size

		":main", "jump", // TODO proper entry point

		// used   [10]uint32 @0x0100    TODO use a bit vector
		// values [8]uint32  @0x0140
		// 0 1 2 3 4 5 6 7
		// d e y n r o s m

		"main:",

		0x0140, "cpush", 0x0140+4*8, "cpush", // : 0x0140 0x0160

		//// d + e = y  (mod 10)

		0x0140+4*0, "push", ":choose", "call", // $d :
		0x0140+4*1, "push", ":choose", "call", // $d $e :
		"add", "dup", // $d+e $d+e :
		10, "mod", // $d+e ($d+e)%10 :
		"dup", 0x0140+4*2, "storeTo", // $d+e $y :   -- $y=($d+e)%10
		":markUsed", "call", // $d+e :
		10, "div", // carry :

		//// carry + n + r = e  (mod 10)

		"dup",               // carry carry :
		0x0140+4*1, "fetch", // carry carry $e :
		"swap",                                // carry $e carry :
		0x0140+4*3, "push", ":choose", "call", // carry $e carry $n :
		"add", "sub", 10, "mod", // carry ($e-(carry+$n))%10 :
		"dup", 0x0140+4*4, "storeTo", // carry $r :   -- $r=($e-(carry+$n))%10
		":markUsed", "call", // carry :
		0x0140+4*3, "fetch", // carry $n :
		0x0140+4*4, "fetch", // carry $n $r :
		"add", "add", 10, "div", // carry :

		//// carry + e + o = n  (mod 10)

		"dup",               // carry carry :
		0x0140+4*1, "fetch", // carry carry $e :
		"add",               // carry carry+$e :
		0x0140+4*3, "fetch", // carry carry+$e $n :
		"swap", "sub", // carry $n-(carry+$e) :
		10, "mod", // carry ($n-(carry+$e))%10 :
		"dup", 0x0140+4*5, "storeTo", // carry $o :   -- $o=($n-(carry+$e))%10
		":markUsed", "call", // carry :
		0x0140+4*1, "fetch", // carry $e :
		0x0140+4*5, "fetch", // carry $e $o :
		"add", "add", 10, "div", // carry :

		//// carry + s + m = o  (mod 10)

		"dup",                                 // carry carry :
		0x0140+4*6, "push", ":choose", "call", // carry carry $s :
		"add",               // carry carry+$s :
		0x0140+4*5, "fetch", // carry carry+$s $o :
		"swap", "sub", // carry $o-(carry+$s) :
		10, "mod", // carry ($o-(carry+$s))%10 :
		"dup", 0x0140+4*7, "storeTo", // carry $m :   -- $m=($o-(carry+$s))%10
		":markUsed", "call", // carry :
		0x0140+4*6, "fetch", // carry $s :
		"dup", 1, "hz", // carry $s :   -- guard $s != 0
		0x0140+4*7, "fetch", // carry $s $m :
		"dup", 1, "hz", // carry $s $m :   -- guard $m != 0
		"add", "add", 10, "div", // carry :

		//// carry = m  (mod 10)
		0x0140+4*7, "fetch", // carry $m
		"eq", 3, "hz",

		//// Done
		0, "halt",

		"choose:",                        // &$X : retIp
		0, "push", ":chooseLoop", "jump", // &$X i=0 : retIp
		"chooseNext:", 1, "add", // &$X i++ : retIp
		"chooseLoop:",                        // &$X i : retIp
		"dup", 9, "lt", ":chooseNext", "fnz", // &$X i : retIp   -- fork next if i < 9
		"dup", 2, "swap", "storeTo", // $X=i : retIp
		"dup", // $X $X : retIP   -- dup as arg for fallsthrough to markUsed

		"markUsed:",                     // $X : retIp
		4, "mul", 0x0100, "push", "add", // ... &used[$X]
		"dup", "fetch", // ... &used[$X] used[$X]
		2, "hnz", // ... &used[$X]
		1, "store", // ... -- used[$X] = 1
		"ret", // :

	),

	Result: Results{
		{Values: [][]uint32{{
			7, // d
			5, // e
			2, // y
			6, // n
			8, // r
			0, // o
			9, // s
			1, // m
		}}},
	}.WithExpectedHaltCodes(1, 2, 3),
}

func TestMach_send_more_money(t *testing.T)      { smmTest.Run(t) }
func BenchmarkMach_send_more_money(b *testing.B) { smmTest.Bench(b) }
