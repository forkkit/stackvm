package stackvm_test

import (
	"fmt"
	"testing"

	. "github.com/jcorbin/stackvm/x"
)

func TestMach_collatz_sequence(t *testing.T) {
	// Test that the vm can generate the collatz sequence from a given starting
	// point.

	tcs := make(TestCases, 0, 9)
	for n := 1; n < 10; n++ {
		// compute the expected collatz sequence for n
		vals := []uint32{uint32(n)}
		val := vals[0]
		for {
			switch {
			case val%2 == 0:
				val = val / 2
			default:
				val = 3*val + 1
			}
			vals = append(vals, val)
			if val <= 1 {
				break
			}
		}

		// build the test case for n
		tcs = append(tcs, TestCase{
			Name: fmt.Sprintf("collatz(%d)", n),
			Prog: []interface{}{
				n, "push", "dup", // v v :
				0x100, "push", // v v i :
				"dup", 4, "add", "p2c", // v v i : i=i+4
				"storeTo", // v : i

				"loop:",         // v : i
				"dup", 2, "mod", // v v%2 : ...

				":odd", "jnz",

				"even:",
				2, "div", // v/2 : ...
				":next", "jump",

				"odd:",
				3, "mul", 1, "add", // 3*v+1 : ...

				"next:",
				"dup",    // v v : i
				"c2p",    // v v i :
				"dup",    // v v i i :
				4, "add", // v v i i+4 :
				"p2c",     // v v i : i=i+4
				"storeTo", // v : i
				"dup",     // v v : i
				1, "eq",   // v v==1 : i
				":loop", "jz", // v : i

				"c2p",         // v i :
				0x100, "push", // v i base :
				2, "p2c", // v : i base
				"halt",
			},
			Result: Result{
				Values: [][]uint32{vals},
			},
		})
	}

	tcs.Run(t)
}

// Test that the vm can reverse explore the collatz recurrence space to some
// depth; initialize a depth counter to 6, then for any given n:
// - always explore 2*n and - if 3 divides n-1 alse explore (n-1)/3
// - accumulate n into memory, like the sequence generator did
// - decrement the depth counter, halting if it reaches 0

var collatzExplore = TestCase{
	Name: "gen collatz",
	Prog: []interface{}{
		6, "push", // d :
		0x100, "push", // d i :
		0x100, "push", // d i b :
		3, "p2c", // : i d i
		1, "push", // v=1 : b i d

		"round:", // v : i d

		"dup", 1, "sub", 3, "mod", // v (v-1)%3 : i d
		":third", "fz", // v : i d
		"double:", 2, "mul", // v=2*v : i d
		":next", "jump", // ...
		"third:", 1, "sub", 3, "div", // v=(v-1)/3 : i d

		"next:",        // v : i d
		"dup", 1, "hz", // v : i d

		"dup",    // v v : i d
		2, "c2p", // v v d i :
		"dup", 4, "add", "p2c", // v v d i : i+=4
		"swap",    // v v i d : i
		"p2c",     // v v i : i d
		"storeTo", // v : i d

		"c2p", 1, "sub", // v d-- : i
		"dup", "p2c", 0, "gt", // v d>0 : i d
		":round", "jnz", // v : i d

		"pop", "cpop", "halt", // : i
	},

	Result: Results{
		{Values: [][]uint32{{2, 4, 8, 16, 32, 64}}},
		{Values: [][]uint32{{2, 4, 8, 16, 5, 10}}},
		{Values: [][]uint32{{2, 4, 1, 2, 4, 8}}},
		{Values: [][]uint32{{2, 4, 1, 2, 4, 1}}},
	}.WithExpectedHaltCodes(1),
}

func TestMach_collatz_explore(t *testing.T)      { collatzExplore.Run(t) }
func BenchmarkMach_collatz_explore(b *testing.B) { collatzExplore.Bench(b) }
