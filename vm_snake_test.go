package stackvm_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	. "github.com/jcorbin/stackvm/x"
)

var snakeSupportLib = []interface{}{
	// forall returns N times for ever lo <= n <= hi
	"forall:",             // lo hi : retIp
	"swap",                // hi v=lo : retIp
	":forallLoop", "jump", // hi v : retIp
	"forallNext:", // hi v : retIp
	1, "add",      // hi v++ : retIp
	"forallLoop:",   // hi v : retIp
	"dup", 3, "dup", // hi v v hi : retIp
	"lt",                 // hi v v<hi : retIp
	":forallNext", "fnz", // hi v : retIp   -- fork next if v < hi
	"swap", "pop", // v : retIp
	"ret", // v :

	"i2xyz:",        // i : retIp
	"dup", 3, "mod", // i x=i%3 : retIp
	"swap", 3, "div", // x i/3 : retIp
	"dup", 3, "mod", // x i/3 y=i/3%3 : retIp
	"swap", 3, "div", // x y z=i/3/3 : retIp
	"ret", // x y z :

	"xyz2i:", // x y z : retIp
	3, "mul", // x y 3*z : retIp
	"add",    // x y+3*z : retIp
	3, "mul", // x 3*(y+3*z) : retIp
	"add", // i=x+3*(3*z+y) : retIp
	"ret", // i : retIp

	"vec3addptr:", // x y z p=*[3]uint32 : retIp
	3, "swap",     // p y z x : retIp
	4, "dup", "fetch", // p y z x dx=*p : retIp
	"add",     // p y z x+dx : retIp
	3, "swap", // x+dx y z p : retIp
	4, "add", // x+dx y z p+=4 : retIp
	2, "swap", // x+dx p z y : retIp
	3, "dup", "fetch", // x+dx p z y dy=*p : retIp
	"add",     // x+dx p z y+dy : retIp
	2, "swap", // x+dx y+dy z p : retIp
	4, "add", // x+dx y+dy z p+=4 : retIp
	"fetch", // x+dx y+dy z dz=*p : retIp
	"add",   // x+dx y+dy z+dz : retIp
	"ret",   // x+dx y+dy z+dz :
}

func Test_snake_forAll(t *testing.T) {
	prog := MustAssemble(
		".data",
		".in", "range:", 0, 0,
		".out", "value:", 0,
		".include", snakeSupportLib,
		".entry", "main:",
		0, ":range", "fetch", // lo :
		4, ":range", "fetch", // lo hi :
		":forall", "call", // i :
		"dup", "mul", // i * i :
		":value", "storeTo", // :
		"halt", // :
	)
	TestCases{
		{
			Name:   "square answer",
			Prog:   prog,
			Input:  map[string][]uint32{"range": {42, 42}},
			Result: Result{Values: map[string][]uint32{"value": {1764}}},
		},
		{
			Name:  "first 10 squares",
			Prog:  prog,
			Input: map[string][]uint32{"range": {0, 9}},
			Result: Results{
				{Values: map[string][]uint32{"value": {0}}},
				{Values: map[string][]uint32{"value": {1}}},
				{Values: map[string][]uint32{"value": {4}}},
				{Values: map[string][]uint32{"value": {9}}},
				{Values: map[string][]uint32{"value": {16}}},
				{Values: map[string][]uint32{"value": {25}}},
				{Values: map[string][]uint32{"value": {36}}},
				{Values: map[string][]uint32{"value": {49}}},
				{Values: map[string][]uint32{"value": {64}}},
				{Values: map[string][]uint32{"value": {81}}},
			},
		},
	}.Run(t)
}

func Test_snakeCube(t *testing.T) {
	N := 3
	rng := makeFastRNG(15517)

	for i := 0; i < 4; i++ {
		rows := genSnakeCubeRows(rng, N)
		labels := labelcells(rows)

		// fmt.Println(rows)
		// for i, label := range renderRowLabels(rows, labels) {
		// 	fmt.Printf("%v: %s\n", rows[i], label)
		// }

		M := len(labels)

		code := []interface{}{
			".maxOps", 1000,
			".maxCopies", 1000,

			//// definitions and setup
			".data",

			// unit vectors in x,y,z space. Strategically laid out such that a
			// direction and its opposite are congruent index-mod-9. The
			// index-mod-9 property lets us quickly check for 'not same or
			// opposite direction' later on.
			"vectors:",
			1, 0, 0,
			0, 1, 0,
			0, 0, 1,
			-1, 0, 0,
			0, -1, 0,
			0, 0, -1,

			// occupied cube cell bitvector
			".out", "occupied:", ".alloc", int(math.Ceil(math.Log2(float64(N*N*N)) / 8 / 4)),

			// starting index in the cube
			".out", "start:", 0,

			// chosen orientation for each fixed-chain head
			".out", "choices:", ".alloc", M,

			".text",

			".include", snakeSupportLib,

			//// choose starting position
			// TODO: prune using some symmetry (probably we can get away with
			// only one boundary-inclusive oct of the cube)

			".entry", "chooseStart:",
			0, "push", N - 1, "push", ":forall", "call", // xi :
			0, "push", N - 1, "push", ":forall", "call", // xi yi :
			0, "push", N - 1, "push", ":forall", "call", // xi yi zi :

			//// compute starting index

			3, "mul", // xi yi 3*zi :
			"add",    // xi yi+3*zi :
			3, "mul", // xi 3*(yi+3*zi) :
			"add", // xi+3*(yi+3*zi) :   -- i=...

			//// choose initial direction: at first all of them are possible

			"choose_0:",
			0, "push", 5, "push", ":forall", "call", // i vi :
			"dup",               // i vi vi :
			":start", "storeTo", // i vi :   -- start=vi
			"swap", // vi i :
		}

		for i := 1; i < M; i++ {
			cl := labels[i]
			switch {
			case cl&(rowHead|colHead) != fixedCell:
				// choose next orientation
				code = append(code,
					fmt.Sprintf("choice_%d:", i), // vi i :
					"swap",                                  // i lastVi=vi :
					0, "push", 5, "push", ":forall", "call", // i lastVi vi :
					"dup",     // i lastVi vi vi :
					2, "swap", // i vi lastVi vi :
					3, "mod", // i vi lastVi vi%3 :
					"swap",   // i vi vi%3 lastVi :
					3, "mod", // i vi vi%3 lastVi%3 :
					"eq",     // i vi vi%3==lastVi%3 :
					1, "hnz", // i vi :  -- halt if ...
					"dup",                      // i vi vi :
					4*i, ":choices", "storeTo", // i vi :   -- choices[i]=vi
					"swap", // vi i :
				)

				// TODO: micro perf faster to avoid forking, rather than
				// fork-and-guard... really we need to have a filtered-forall,
				// or forall-such-that in whatever higher level language we
				// start building Later ™

				// TODO: surely there's some way to prune this also:
				// - at the very last, don't choose vectors that point out a
				//   cube face, since they'll just fail the range check soon to
				//   come
				// - more advanced, also use the row counts, and prune ones
				//   that will fail any range check before the next freedom
				// - these could actually eliminate the need for range checks
			}

			code = append(code,
				fmt.Sprintf("advance_%d:", i), // vi i :
				":i2xyz", "call",              // vi x y z :
				4, "dup", 3, "mul", // vi x y z 3*vi :
				4, "mul", ":vectors", "add", // vi x y z &vectors[3*vi] :
				":vec3addptr", "call", // vi x y z :   -- x,y,z now incremented by the vector
				":xyz2i", "call", // vi i :
				"dup", 0, "lt", // vi i i<0 :
				2, "hnz", // vi i :   -- halt if ...
				"dup", N*N*N, "gte", // vi i i>=N^3 :
				2, "hnz", // vi i :   -- halt if ...
				"dup",                  // vi i i :
				":occupied", "bitseta", // vi i !occupied[i] :   -- set occupied[i] if it wasn't
				3, "hz", // vi i :   -- halt if unable to set bit
			)
		}

		code = append(code,
			"done:",  // i v :
			2, "pop", // :
			":start", "cpush", ":choices", "cpush", // : &start &choices
			":choices", "cpush", 4*M, ":choices", "cpush", // : &start &choices &choices[0] &choices[M]
			"halt", // : &start &choices &choices[0] &choices[M]
		)

		// dumpCode(code)

		tc := TestCase{
			Name:   fmt.Sprintf("snake %v", rows),
			Prog:   MustAssemble(code...),
			Result: NoResult.WithExpectedHaltCodes(1, 2, 3),
		}
		t.Run(tc.Name, tc.Run)
	}
}

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

// TODO: factor out some sort of x/codedumper
func dumpCode(code []interface{}) {
	cont := false
	for _, c := range code {
		if s, ok := c.(string); ok {
			if cont && strings.HasSuffix(s, ":") {
				fmt.Printf("\n")
				cont = false
			}
		}
		if cont {
			fmt.Printf(" ")
		}
		fmt.Printf(fmt.Sprint(c))
		cont = true
		if s, ok := c.(string); ok {
			if s == "ret" {
				fmt.Printf("\n\n")
				cont = false
			}
		}
	}
	if cont {
		fmt.Printf("\n\n")
		cont = false
	}
}
