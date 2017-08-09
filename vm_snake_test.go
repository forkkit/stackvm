package stackvm_test

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/jcorbin/stackvm/x"
)

func Test_snakeCube(t *testing.T) {
	N := 3
	rng := makeFastRNG(15517)

	for i := 0; i < 4; i++ {
		rows := genSnakeCubeRows(rng, N)
		fmt.Println(rows)
		labels := labelcells(rows)

		for i, label := range renderRowLabels(rows, labels) {
			fmt.Printf("%v: %s\n", rows[i], label)
		}

		code := []interface{}{
			0x40, // stack size

			"halt",
		}

		// definitions and setup
		fmt.Printtf("// unit vectors in x,y,z space, mapped to one-dimensional space under the\n")
		fmt.Printtf("// row-major convention. Strategically laid out such that a direction and its\n")
		fmt.Printtf("// opposite are congruent index-mod-3. The index-mod-3 property lets us quickly\n")
		fmt.Printtf("// check for 'not same or opposiite direction' later on.\n")
		fmt.Printf("# const vectors = [1, 3, 9, -1, -3, -9]\n")

		fmt.Printf("# alloc [%d]choices\n", len(labels))

		// choose starting position
		fmt.Printf("# forall xi := 0; xi < %d; xi++\n", N)
		fmt.Printf("# forall yi := 0; yi < %d; yi++\n", N)
		fmt.Printf("# forall zi := 0; zi < %d; zi++\n", N)
		// TODO: prune using some symmetry (probably we can get away with only
		// one boundary-inclusive oct of the cube)
		fmt.Printf("# i := xi+3*(yi+3*zi)\n")

		fmt.Printf("# forall vi, di := range vectors\n")
		fmt.Printf("# choices[%d] = vi\n", i)

		lastChoice := 0
		for i := 1; i < len(labels); i++ {
			cl := labels[i]
			fmt.Printf("## [%d]: %v\n", i, cl)
			switch {
			case cl&(rowHead|colHead) != fixedCell:
				// choose orientation
				fmt.Printf("# forall vi, di := range vectors\n")
				fmt.Printf("# halt EENCONCEIVABLE if vi%%3 == choices[%d]%%3\n", lastChoice)
				// TODO: micro perf faster to avoid forking, rather than
				// fork-and-guard... really we need to have a filtered-forall,
				// or forall-such-that in whatever higher level language we
				// start building Later ™

				fmt.Printf("# choices[%d] = vi\n", i)
				// TODO: surely there's some way to prune this also:
				// - at the very last, don't choose vectors that point out a
				//   cube face, since they'll just fail the range check soon to
				//   come
				// - more advanced, also use the row counts, and prune ones
				//   that will fail any range check before the next freedom
				// - these could actually eliminate the need for range checks

				lastChoice = i
			}

			fmt.Printf("# i += di\n")
			fmt.Printf("# halt ERANGE if i < 0 || i >= %d\n", N*N*N)
		}

		fmt.Println()

		MustAssemble(code...)
	}
}

/*

var snakeSolTest = TestCase{
	Name: "snake XXX",
	Prog: MustAssemble(
		0x40,
		//// Done
		0, "halt",
	),
	// Result: XXX,
}

*/

// labelcells generates a list of cell labels given a list of row counts.
//
// rows is simply a list of cell counts per row that describes a possible snake
// (its ability to actually form a cube is another matter). For example,
// consider the trivial 2x2x2 cube, one of the few possible snakes would be [2,
// 1, 2, 1, 2], which can be visualized like:
//  # #
// 	  #
// 	  # #
// 		#
// 		# #
//
// The labels emitted are one of:
// - rH / rT : the cell is the head or tail of a row freedom
// - cH / cT : the cell is the head or tail of a column freedom
// - #       : the cell is not part of a freedom
func labelcells(rows []int) []cellLabel {
	n := 0
	for _, row := range rows {
		n += row
	}
	r := make([]cellLabel, n)

	head, tail := 0, 0
	for _, row := range rows {
		// pending column terminates if non-trivial row, or final
		if head < tail && (row > 1 || tail == len(r)-1) {
			r[head] |= colHead
			r[tail] |= colTail
			head = tail
		}

		// mark row head and tail
		if row > 1 {
			tail += row - 1
			r[head] |= rowHead
			r[tail] |= rowTail
			head = tail // its tail becomes the next potential column head
		}

		// advance tail to point to next row head
		tail++
	}

	return r
}

type cellLabel uint8

const (
	fixedCell cellLabel = 0
	rowHead   cellLabel = 1 << iota
	rowTail
	colHead
	colTail
)

func (cl cellLabel) String() string {
	if cl == fixedCell {
		return "#"
	}

	parts := make([]string, 0, 6)

	switch cl & (rowHead | rowTail) {
	case rowHead:
		parts = append(parts, "rH")
		cl &= ^rowHead
	case rowTail:
		parts = append(parts, "rT")
		cl &= ^rowTail
	}

	switch cl & (colHead | colTail) {
	case colHead:
		parts = append(parts, "cH")
		cl &= ^colHead
	case colTail:
		parts = append(parts, "cT")
		cl &= ^colTail
	}

	if cl != 0 {
		return fmt.Sprintf("!<%d>!", cl)
	}

	return strings.Join(parts, ":")
}

func genSnakeCubeRows(rng fastRNG, m int) []int {
	n := m * m * m
	r := make([]int, 0, n)
	i := 0
	run := 0
	for i < n {
		var c int
		for {
			c = 1 + int(rng.next()%3)
			if i+c > n {
				continue
			}
			if c == 1 {
				if run >= 3 {
					continue
				}
				run++
			} else {
				run = 2
			}
			break
		}
		i += c
		r = append(r, c)
	}
	return r
}

func renderRowLabels(rows []int, cls []cellLabel) []string {
	rls := make([][]string, len(rows))

	// render cell labels grouped by row counts
	k := 0 // cursor in cls
	for i, row := range rows {
		rl := make([]string, row)
		for j := 0; j < row; j++ {
			rl[j] = cls[k].String()
			k++
		}
		rls[i] = rl
	}

	// pad columns
	var (
		w    int
		last []string
	)
	for _, rl := range rls {
		if len(rl[0]) < w {
			rl[0] = strings.Repeat(" ", w-len(rl[0])) + rl[0]
		}
		if w > 0 && w < len(rl[0]) {
			last[len(last)-1] = strings.Repeat(" ", len(rl[0])-w) + last[len(last)-1]
		}
		w = len(rl[len(rl)-1])
		last = rl
	}

	r2 := make([]string, len(rls))
	var prefix string
	for i, rl := range rls {
		label := strings.Join(rl, " ")
		r2[i] = prefix + label
		prefix += strings.Repeat(" ", len(label)-len(rl[len(rl)-1]))
	}
	return r2
}

// fastRNG is just a fixed LCG; TODO: add a PCG twist, choose a better M.
type fastRNG struct{ state *uint32 }

func makeFastRNG(seed uint32) fastRNG { return fastRNG{state: &seed} }

func (fr fastRNG) next() uint32 {
	const (
		M = 134775813
		C = 1
	)
	n := *fr.state
	n = M*n + C
	*fr.state = n
	return n
}
