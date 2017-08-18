package stackvm_test

import (
	"testing"

	. "github.com/jcorbin/stackvm/x"
)

// These tests are essentially "unit" tests operations and/or features of the
// vm.

// So far my testing strategy has been to write end-to-end or "integration"
// tests since it's been a decent trade-off of time to outcome, and it forced
// building tracing to debug failures. Going forward tho, I'd like to start
// writing more targeted/smaller "unit" tests that exercise one op or vm feature.

func TestMach_misc_ops(t *testing.T) {
	TestCases{
		{
			Name: "nuthin' doin'",
			Prog: MustAssemble(
				"nop", "nop", "nop", "nop",
				"halt",
			),
		},
	}.Run(t)
}

func TestMach_basic_math(t *testing.T) {
	TestCases{
		{
			Name: "33addeq5 should fail",
			Err:  "HALT(1)",
			Prog: MustAssemble(
				3, "push", 3, "push", "add",
				5, "push", "eq",
				1, "hz", "halt",
			),
			Result: Result{
				Err: "HALT(1)",
			},
		},

		{
			Name: "23addeq5 should succeed",
			Prog: MustAssemble(
				2, "push", 3, "push", "add",
				5, "push", "eq",
				1, "hz", "halt",
			),
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
				0x00,       // version
				0xc0, 0x01, // stack size
				0x7f, // end-of-options
				0x70, // undefined op code
			},
			Result: Result{Err: "invalid op UNDEFINED<0x70>"},
		},
		{
			Name: "crash: explicit",
			Err:  "crashed",
			Prog: []byte{
				0x00,       // version
				0xc0, 0x01, // stack size
				0x7f, // end-of-options
				0x00, // opCodeCrash=0
			},
			Result: Result{Err: "crashed"},
		},
		{
			Name: "crash: implicit",
			Err:  "crashed",
			Prog: []byte{
				0x00,       // version
				0xc0, 0x01, // stack size
				0x7f, // end-of-options
				// empty program, 0 by default
			},
			Result: Result{Err: "crashed"},
		},
		{
			Name: "crash: jump out of program",
			Err:  "crashed",
			Prog: MustAssemble(
				96, "jump", "halt",
			),
			Result: Result{Err: "crashed"},
		},
		{
			Name: "crash: implicit assembled",
			Err:  "crashed",
			Prog: MustAssemble(
				1, "push",
				2, "add",
				// and then?...
			),
			Result: Result{Err: "crashed"},
		},
		{
			Name: "maxops stops an infinite loop",
			Err:  "op count limit exceeded",
			Prog: MustAssemble(
				".maxOps", 100,
				1, "push",
				"loop:",
				1, "add",
				":loop", "jump",
				0, "halt",
			),
			Result: Result{Err: "op count limit exceeded"},
		},
	}.Run(t)
}

func TestMach_data_refs(t *testing.T) {
	TestCases{
		{
			Name: "mod-10 check",
			Prog: MustAssemble(
				".data",
				"d:", 4, 2, 7, 9, 8,

				".text",
				".entry", "main:",
				":d", "fetch", // d[0] :
				4*1, ":d", "push", "fetch", // d[0] d[1] :
				4*2, ":d", "push", "fetch", // d[0] d[1] d[2] :
				4*3, ":d", "push", "fetch", // d[0] d[1] d[2] d[3] :
				4*4, ":d", "push", "fetch", // d[0] d[1] d[2] d[3] d[4] :
				"add", "add", "add", "add", // s=d[0]+d[1]+d[2]+d[3]+d[4] :
				10, "mod", // s%10 :
				1, "hnz", // : -- error halt if non-zero
				"halt", // : normal halt
			),
		},
	}.Run(t)
}

func TestMach_bitwise_ops(t *testing.T) {
	TestCases{
		{
			Name: "masking",
			Prog: MustAssemble(
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
			),
		},

		{
			Name: "bitand",
			Prog: MustAssemble(
				0xff, "push", 0x12, "push", "bitand",
				0x12, "eq", 1, "hz",
				0x0f, "push", 0x12, "bitand",
				0x02, "eq", 1, "hz",
				"halt",
			),
		},

		{
			Name: "bitor",
			Prog: MustAssemble(
				1, "push", 2, "push", "bitor",
				3, "eq", 1, "hz",
				3, "push", 6, "bitor",
				7, "eq", 1, "hz",
				"halt",
			),
		},

		{
			Name: "bitxor",
			Prog: MustAssemble(
				0x42, "push",
				0x99, "push", "bitxor",
				0xed, "bitxor",
				"dup", 0x42^0x99^0xed, "eq", 1, "hz",

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
			),
		},

		{
			Name: "bit set & test & clear",
			Prog: MustAssemble(
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

				"halt",

				// 4 * 32 = 128 bits
				"vec:", ".data", ".alloc", 4,
			),
		},
	}.Run(t)
}
