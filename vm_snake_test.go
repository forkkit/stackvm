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
	// rowlabels := labelrows(snake)

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

	rng := makeFastRNG(15517)
	for i := 0; i < 4; i++ {
		rows := genSnakeCubeRows(rng, 3)
		fmt.Println(rows)
		rowlabels := labelrows(rows)
		for i, label := range renderRowLabels(rowlabels) {
			fmt.Printf("%v: %s\n", rows[i], label)
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

// labelrows generates a list of row labels given a list of row counts.
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
func labelrows(rows []int) []cellabel {
	n := 0
	for _, row := range rows {
		n += row
	}
	r := make([]cellabel, n)

	head, tail := 0, 0
	for i, row := range rows {
		// if we're in a column, continue seeking its tail
		if row == 1 {
			tail++
			continue
		}

		// we're in a non-trivial row, it terminates any column gap
		if head < tail {
			rl[head] |= colHead
			rl[tail] |= colTail
		}

		// mark its head and tail
		head, tail = tail, tail+row
		rl[head] |= rowHead
		rl[tail] |= rowTail

		// its tail becomes the next potential column head
		head = tail
	}

	// mark any final column
	if head < tail {
		rl[head] |= colHead
		rl[tail] |= colTail
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

func renderRowLabels(cls []cellabel) []string {
	// FIXME

	r := make([][]string, len(rls))
	for i, rl := range rls {
		ri := make([]string, len(rl))
		for j, cl := range rl {
			ri[j] = cl.String()
		}
		r[i] = ri
	}

	var (
		w    int
		last []string
	)
	for _, rl := range r {
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
	for i, rl := range r {
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
