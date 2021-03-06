package stackvm_test

import (
	"fmt"
	"testing"

	. "github.com/jcorbin/stackvm/x"
	"github.com/stretchr/testify/assert"
)

// These tests are essentially "unit" tests operations and/or features of the
// vm.

// So far my testing strategy has been to write end-to-end or "integration"
// tests since it's been a decent trade-off of time to outcome, and it forced
// building tracing to debug failures. Going forward tho, I'd like to start
// writing more targeted/smaller "unit" tests that exercise one op or vm feature.

func TestAssembler(t *testing.T) {
	for _, tc := range []struct {
		name string
		prog []interface{}
		code []byte
		err  string
	}{
		{
			name: "undefined jump label",
			prog: []interface{}{
				":nope", "jump",
			},
			err: `undefined labels: ["nope"]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err := Assemble(tc.prog...)
			if tc.err != "" {
				assert.EqualError(t, err, tc.err, "expected error")
				return
			}
			if assert.NoError(t, err, "unexpected error") {
				assert.Equal(t, tc.code, code, "expected machine code")
			}
		})
	}
}

func TestMach_stack_ops(t *testing.T) {
	// TODO: more basic coverage
	TestCases{
		{
			Name: "push",
			Prog: []interface{}{"push", 1, "hnz", "halt"},
		},
		{
			Name: "0 push",
			Prog: []interface{}{0, "push", 1, "hnz", "halt"},
		},
		{
			Name: "1 push",
			Prog: []interface{}{1, "push", 1, "hz", "halt"},
		},
	}.Run(t)
}

func TestMach_misc_ops(t *testing.T) {
	TestCases{
		{
			Name: "nuthin' doin'",
			Prog: []interface{}{
				"nop", "nop", "nop", "nop",
				"halt",
			},
		},
	}.Run(t)
}

func TestMach_basic_math(t *testing.T) {
	TestCases{
		{
			Name: "33addeq5 should fail",
			Err:  "HALT(1)",
			Prog: []interface{}{
				3, "push", 3, "push", "add",
				5, "push", "eq",
				1, "hz", "halt",
			},
			Result: Result{
				Err: "HALT(1)",
			},
		},

		{
			Name: "23addeq5 should succeed",
			Prog: []interface{}{
				2, "push", 3, "push", "add",
				5, "push", "eq",
				1, "hz", "halt",
			},
			Result: Result{},
		},
	}.Run(t)
}

func TestMach_operational_errors(t *testing.T) {
	TestCases{
		{
			Name: "invalid op code",
			Err:  "invalid op UNDEFINED<0x70>",
			Prog: []byte{
				0x00, // end-of-options
				0x70, // undefined op code
			},
			Result: Result{Err: "invalid op UNDEFINED<0x70>"},
		},
		{
			Name: "crash: explicit",
			Err:  "crashed",
			Prog: []byte{
				0x00, // end-of-options
				0x00, // opCodeCrash=0
			},
			Result: Result{Err: "crashed"},
		},
		{
			Name: "crash: implicit",
			Err:  "crashed",
			Prog: []byte{
				0x00, // end-of-options
				// empty program, 0 by default
			},
			Result: Result{Err: "crashed"},
		},
		{
			Name: "crash: jump out of program",
			Err:  "crashed",
			Prog: []interface{}{
				96, "jump", "halt",
			},
			Result: Result{Err: "crashed"},
		},
		{
			Name: "crash: implicit assembled",
			Err:  "crashed",
			Prog: []interface{}{
				1, "push",
				2, "add",
				// and then?...
			},
			Result: Result{Err: "crashed"},
		},
		{
			Name: "maxops stops an infinite loop",
			Err:  "op count limit exceeded",
			Prog: []interface{}{
				".maxOps", 100,
				1, "push",
				"loop:",
				1, "add",
				":loop", "jump",
				0, "halt",
			},
			Result: Result{Err: "op count limit exceeded"},
		},
		{
			Name: "maxcopies stops an infinite copy loop",
			Prog: []interface{}{
				".maxCopies", 100,
				"foo:", ":bar", "fork", 1, "halt",
				"bar:", ":foo", "fork", 2, "halt",
				3, "halt",
			},
			Result: Result{
				Err: "max copies(100) exceeded",
			}.WithExpectedHaltCodes(1, 2),
		},
	}.Run(t)
}

func TestMach_data_refs(t *testing.T) {
	TestCase{
		Name: "mod-10 check",
		Prog: []interface{}{
			".data",
			"d:", 4, 2, 7, 9, 8,

			".text",
			".entry", "main:",
			":d", "fetch", // d[0] :
			4 * 1, ":d", "push", "fetch", // d[0] d[1] :
			4 * 2, ":d", "push", "fetch", // d[0] d[1] d[2] :
			4 * 3, ":d", "push", "fetch", // d[0] d[1] d[2] d[3] :
			4 * 4, ":d", "push", "fetch", // d[0] d[1] d[2] d[3] d[4] :
			"add", "add", "add", "add", // s=d[0]+d[1]+d[2]+d[3]+d[4] :
			10, "mod", // s%10 :
			1, "hnz", // : -- error halt if non-zero
			"halt", // : normal halt
		},
	}.Run(t)
}

func TestMach_bitwise_ops(t *testing.T) {
	TestCases{
		{
			Name: "masking",
			Prog: []interface{}{
				0xdead, "push", 16, "shiftl",
				0xbeef, "bitor",
				"dup", 0xdeadbeef, "eq", 1, "hz",

				"dup", 0xffff, "bitand",
				0xbeef, "eq", 1, "hz",

				"dup",
				0xffff, "push", "bitnot", "bitand",
				16, "shiftr",
				0xdead, "eq", 1, "hz",

				"halt",
			},
		},

		{
			Name: "bitand",
			Prog: []interface{}{
				0xff, "push", 0x12, "push", "bitand",
				0x12, "eq", 1, "hz",
				0x0f, "push", 0x12, "bitand",
				0x02, "eq", 1, "hz",
				"halt",
			},
		},

		{
			Name: "bitor",
			Prog: []interface{}{
				1, "push", 2, "push", "bitor",
				3, "eq", 1, "hz",
				3, "push", 6, "bitor",
				7, "eq", 1, "hz",
				"halt",
			},
		},

		{
			Name: "bitxor",
			Prog: []interface{}{
				0x42, "push",
				0x99, "push", "bitxor",
				0xed, "bitxor",
				"dup", 0x42 ^ 0x99 ^ 0xed, "eq", 1, "hz",

				"dup",
				0x99, "bitxor",
				0xed, "bitxor",
				0x42, "eq", 1, "hz",

				"dup",
				0xed, "bitxor",
				0x42, "bitxor",
				0x99, "eq", 1, "hz",

				"dup",
				0x42, "bitxor",
				0x99, "bitxor",
				0xed, "eq", 1, "hz",

				"halt",
			},
		},

		{
			Name: "bit set & test & clear",
			Prog: []interface{}{
				// set some bits
				40, "push", ":vec", "bitset",
				42, "push", ":vec", "push", "bitset",
				99, "push", ":vec", "bitset",

				// test for them, and some near misses
				39, "push", ":vec", "bitest", 1, "hnz",
				40, "push", ":vec", "bitest", 1, "hz",
				41, "push", ":vec", "bitest", 1, "hnz",
				42, "push", ":vec", "push", "bitest", 1, "hz",
				43, "push", ":vec", "push", "bitest", 1, "hnz",
				98, "push", ":vec", "bitest", 1, "hnz",
				99, "push", ":vec", "bitest", 1, "hz",
				100, "push", ":vec", "bitest", 1, "hnz",

				// clear some bits
				42, "push", ":vec", "bitost",
				99, "push", ":vec", "push", "bitost",

				// test that they're now cleared
				42, "push", ":vec", "push", "bitest", 1, "hnz",
				99, "push", ":vec", "bitest", 1, "hnz",

				// atomic sets
				43, "push", ":vec", "push", "bitseta", 1, "hz",
				43, "push", ":vec", "push", "bitseta", 1, "hnz",
				44, "push", ":vec", "bitseta", 1, "hz",
				44, "push", ":vec", "bitseta", 1, "hnz",

				// atomic clears
				43, "push", ":vec", "push", "bitosta", 1, "hz",
				43, "push", ":vec", "push", "bitosta", 1, "hnz",
				44, "push", ":vec", "bitosta", 1, "hz",
				44, "push", ":vec", "bitosta", 1, "hnz",

				"halt",

				// 4 * 32 = 128 bits
				"vec:", ".data", ".alloc", 4,
			},
		},
	}.Run(t)
}

func TestMach_queueSize(t *testing.T) {
	TestCases{
		{
			Name: "exceeded",
			Prog: []interface{}{
				".queueSize", 1,
				":lol", "fork",
				":wut", "fork",
				0, "halt",
				"lol:", 1, "halt",
				"wut:", 2, "halt",
				"halt",
			},
			Result: Result{
				Err: "run queue full",
			}.WithExpectedHaltCodes(1, 2),
		},
		{
			Name: "sufficient",
			Prog: []interface{}{
				".queueSize", 2,
				":lol", "fork",
				":wut", "fork",
				0, "halt",
				"lol:", 1, "halt",
				"wut:", 2, "halt",
				"halt",
			},
			Result: NoResult.WithExpectedHaltCodes(1, 2),
		},
	}.Run(t)
}

func TestMach_inNout(t *testing.T) {
	prog := MustAssemble(
		".data",
		".in", "N:", 0,
		".out", "M:", 0,

		".entry", "main:",
		":N", "fetch", // N :
		"dup", "mul", // N*N :
		":M", "storeTo", // :   -- M=N*N
		"halt",
	)

	var tcs TestCases
	for n := uint32(0); n < 10; n++ {
		tcs = append(tcs, TestCase{
			Name:   fmt.Sprintf("square(%d)", n),
			Prog:   prog,
			Input:  map[string][]uint32{"N": {n}},
			Result: Result{Values: map[string][]uint32{"M": {n * n}}},
		})
	}
	tcs.Run(t)
}
