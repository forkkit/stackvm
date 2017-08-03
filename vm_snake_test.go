package stackvm_test

import (
	"fmt"
	"strings"
	"testing"
)

// . "github.com/jcorbin/stackvm/x"

func Test_genSnakeCubeRows(t *testing.T) {
	// XXX temp workspace

	// snake := []int{2, 2, 2, 1, 2, 2, 2, 1, 3, 3, 1, 2, 1, 2, 1}
	// labels := labelcells(snake)

	// 2: rH rT:cH
	// 2:    rH:cT rT:cH
	// 2:          rH:cT rT:cH
	// 1:                    #
	// 2:                rH:cT rT:cH
	// 2:                      rH:cT rT:cH
	// 2:                            rH:cT rT:cH
	// 1:                                      #
	// 3:                                  rH:cT # rT:cH
	// 3:                                          rH:cT # rT:cH
	// 1:                                                      #
	// 2:                                                  rH:cT rT:cH
	// 1:                                                            #
	// 2:                                                        rH:cT rT:cH
	// 1:                                                                 cT

	N := 3
	rng := makeFastRNG(15517)

	for i := 0; i < 4; i++ {
		rows := genSnakeCubeRows(rng, N)
		fmt.Println(rows)
		labels := labelcells(rows)

		for i, label := range renderRowLabels(rows, labels) {
			fmt.Printf("%v: %s\n", rows[i], label)
		}

		// definitions and setup
		fmt.Printf("# const vectors = [\n")
		fmt.Printf("#   // laid out such that each direction and its opposite are congruent\n")
		fmt.Printf("#   // index-mod-9 so that we can quickly check for 'not the same or\n")
		fmt.Printf("#   // opposite direction' when selecting a turn heading.\n")
		fmt.Printf("#   0, 0, 1,\n")
		fmt.Printf("#   0, 1, 0,\n")
		fmt.Printf("#   1, 0, 0,\n")
		fmt.Printf("#   0, 0, -1,\n")
		fmt.Printf("#   0, -1, 0,\n")
		fmt.Printf("#   -1, 0, 0,\n")
		fmt.Printf("# ]\n")
		fmt.Printf("# alloc [3]start\n")
		fmt.Printf("# alloc [%d]choices\n", len(labels))

		// choose starting position
		fmt.Printf("# forall xi := 0; xi < %d; xi++\n", N)
		fmt.Printf("# forall yi := 0; yi < %d; yi++\n", N)
		fmt.Printf("# forall zi := 0; zi < %d; zi++\n", N)
		// TODO: prune using some symmetry (probably we can get away with only
		// one boundary-inclusive oct of the cube)
		fmt.Printf("# start[0] = xi\n")
		fmt.Printf("# start[1] = yi\n")
		fmt.Printf("# start[2] = zi\n")

		fmt.Printf("# forall vi := range vectors\n")
		fmt.Printf("# choices[%d] = vi\n", i)
		fmt.Printf("# hx := vectors[vi]\n")
		fmt.Printf("# hy := vectors[vi+1]\n")
		fmt.Printf("# hz := vectors[vi+2]\n")

		lastChoice := 0
		for i := 1; i < len(labels); i++ {
			cl := labels[i]
			fmt.Printf("## [%d]: %v\n", i, cl)
			switch {
			case cl&(rowHead|colHead) != fixedCell:
				// choose orientation
				fmt.Printf("# forall vi := 0; vi < len(vectors); vi+=3\n")
				fmt.Printf("# halt EENCONCEIVABLE if vi%%9 == choices[%d]%%9\n", lastChoice)
				// TODO: micro perf faster to avoid forking, rather than
				// fork-and-guard... really we need to have a filtered-forall,
				// or forall-such-that in whatever higher level language we
				// start building Later ™

				fmt.Printf("# choices[%d] = vi\n", i)
				fmt.Printf("# hx := vectors[vi]\n")
				fmt.Printf("# hy := vectors[vi+1]\n")
				fmt.Printf("# hz := vectors[vi+2]\n")
				// TODO: surely there's some way to prune this also:
				// - at the very last, don't choose vectors that point out a
				//   cube face, since they'll just fail the range check soon to
				//   come
				// - more advanced, also use the row counts, and prune ones
				//   that will fail any range check before the next freedom
				// - these could actually eliminate the need for range checks

				lastChoice = i
			}

			fmt.Printf("# xi += hx\n")
			fmt.Printf("# halt ERANGE if xi < 0 || xi >= %d\n", N)
			fmt.Printf("# yi += hy\n")
			fmt.Printf("# halt ERANGE if yi < 0 || yi >= %d\n", N)
			fmt.Printf("# zi += hz\n")
			fmt.Printf("# halt ERANGE if zi < 0 || zi >= %d\n", N)
		}

		fmt.Println()
	}
}

/*

=== RUN   Test_genSnakeCubeRows

[1 3 1 3 2 2 2 3 1 2 1 3 3]
1:  #
3: rH # rT:cH
1:          #
3:      rH:cT # rT:cH
2:              rH:cT rT:cH
2:                    rH:cT rT:cH
2:                          rH:cT rT:cH
3:                                rH:cT # rT:cH
1:                                            #
2:                                        rH:cT rT:cH
1:                                                  #
3:                                              rH:cT # rT:cH
3:                                                      rH:cT # rT

[3 3 3 1 2 1 3 3 2 3 3]
3: rH # rT:cH
3:      rH:cT # rT:cH
3:              rH:cT # rT:cH
1:                          #
2:                      rH:cT rT:cH
1:                                #
3:                            rH:cT # rT:cH
3:                                    rH:cT # rT:cH
2:                                            rH:cT rT:cH
3:                                                  rH:cT # rT:cH
3:                                                          rH:cT # rT

[3 2 3 2 2 3 2 3 2 1 3 1]
3: rH # rT:cH
2:      rH:cT rT:cH
3:            rH:cT # rT:cH
2:                    rH:cT rT:cH
2:                          rH:cT rT:cH
3:                                rH:cT # rT:cH
2:                                        rH:cT rT:cH
3:                                              rH:cT # rT:cH
2:                                                      rH:cT rT:cH
1:                                                                #
3:                                                            rH:cT # rT:cH
1:                                                                       cT

--- PASS: Test_genSnakeCubeRows (0.00s)
PASS
ok  	github.com/jcorbin/stackvm	0.009s

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
