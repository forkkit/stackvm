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
