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

func TestMach_basic_math(t *testing.T) {
	TestCases{
		{
			Name: "33addeq5 should fail",
			Err:  "HALT(1)",
			Prog: MustAssemble(
				0x40,
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
				0x40,
				2, "push", 3, "push", "add",
				5, "push", "eq",
				1, "hz", "halt",
			),
			Result: Result{},
		},
	}.Run(t)
}
