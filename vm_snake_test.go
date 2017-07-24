package stackvm_test

import (
	"fmt"
	"testing"
	// . "github.com/jcorbin/stackvm/x"
)

const (
	// XXX FIXME (just ick)
	lcgM = uint32(1072301)
	lcgC = uint32(42)
)

func genSnakeCube(rng uint32, m int) []bool {
	n := m * m * m
	limit := n * n

reboot:
	// fmt.Printf("reboot!\n")
	freedoms := make([]bool, 0, n)
	oc := make([]bool, n)
	var lastPos, lastDir [3]int

	oc[0] = true
	freedoms = append(freedoms, true)

	i := 0
	c := 0
	for {
		if c++; c > limit {
			goto reboot
		}
		rng = lcgM*rng + lcgC

		pos := lastPos
		delta := 1
		if rng%2 == 1 {
			delta = -1
		}
		pos[rng%3] += delta

		if pos[0] < 0 || pos[0] >= m || pos[1] < 0 || pos[1] >= m || pos[2] < 0 || pos[2] >= m {
			continue
		}

		j := m*m*pos[0] + m*pos[1] + pos[2]
		if !oc[j] {
			// fmt.Printf("bop [%v] %v\n", i, pos)
			// TODO: trace these choices to build the snake

			dir := [3]int{pos[0] - lastPos[0], pos[1] - lastPos[1], pos[2] - lastPos[2]}
			if lastDir == [3]int{0, 0, 0} {
				freedoms = append(freedoms, false)
			} else if dir == lastDir {
				freedoms = append(freedoms, false)
			} else {
				freedoms = append(freedoms, true)
			}

			lastDir, lastPos, oc[j] = dir, pos, true
			if i++; i >= n-1 {
				break
			}
		}
	}

	// fmt.Printf("done!\n")

	if cap(freedoms) != len(freedoms) {
		panic("genSnakeCube failed to generate a complete snake cube")
	}

	return freedoms
}

func Test_genSnakeCube(t *testing.T) {
	// XXX temporary test to exercise the expectation generator
	fs := genSnakeCube(uint32(0), 3)
	fmt.Println(fs)
}

// XXX snakeGenTest

// var snakeSolTest = TestCase{
// 	Name: "snake XXX",
// 	Prog: MustAssemble(
// 		0x40,

// 		//// Done
// 		0, "halt",
// 	),

// 	// Result: Results{
// 	// 	{Values: [][]uint32{{
// 	// 		7, // d
// 	// 		5, // e
// 	// 		2, // y
// 	// 		6, // n
// 	// 		8, // r
// 	// 		0, // o
// 	// 		9, // s
// 	// 		1, // m
// 	// 	}}},
// 	// }.WithExpectedHaltCodes(1, 2, 3),

// }
