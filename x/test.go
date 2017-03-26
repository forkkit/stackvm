package xstackvm

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jcorbin/stackvm"
)

// TestCases is list of test cases for stackvm.
type TestCases []TestCase

// TestCase is a test case for a stackvm.
type TestCase struct {
	Logf      func(format string, args ...interface{})
	Name      string
	StackSize uint32
	Prog      []byte
	Err       error
	QueueSize int
	Handler   func(*stackvm.Mach) ([]byte, error)
	Results   []Result
	Result    Result
}

// Result represents an expected or actual result within a TestCase.
type Result struct {
	Err error
	PS  []uint32
	CS  []uint32
	Mem []ResultMem
}

// ResultMem represents an expected or actual memory range in a Result.
type ResultMem struct {
	Addr uint32
	Data []byte
}

type testCaseRun struct {
	*testing.T
	TestCase
	res []Result
}

// Run runs each test case in a sub-test.
func (tcs TestCases) Run(t *testing.T) {
	for _, tc := range tcs {
		t.Run(tc.Name, tc.Run)
	}
}

// Trace traces each test case in a sub-test.
func (tcs TestCases) Trace(t *testing.T) {
	for _, tc := range tcs {
		t.Run(tc.Name, tc.Trace)
	}
}

// Run runs the test case; it either succeeds quietly, or fails with a trace
// log.
func (tc TestCase) Run(t *testing.T) {
	run := testCaseRun{
		T:        t,
		TestCase: tc,
	}
	if run.Logf == nil {
		run.Logf = t.Logf
	}
	if run.canaryFailed() {
		run.trace()
	}
}

// Trace runs the test case with trace logging on.
func (tc TestCase) Trace(t *testing.T) {
	run := testCaseRun{
		T:        t,
		TestCase: tc,
	}
	if run.Logf == nil {
		run.Logf = t.Logf
	}
	run.trace()
}

func (t testCaseRun) canaryFailed() bool {
	t.T = &testing.T{}
	m := t.build(t.takeResult)
	t.checkError(m.Run())
	t.checkResults(m, true)
	return t.Failed()
}

func (t testCaseRun) trace() {
	t.Logf("Prog Buffer:")
	t.logLines(hex.Dump(t.Prog))

	m := t.build(t.checkResult)

	var buf bytes.Buffer
	t.Logf("Mach Memory Dump (after load):")
	m.Dump(&buf)
	t.logLines(buf.String())

	trc := LogfTracer(t.Logf)
	t.checkError(m.Trace(trc))

	t.Logf("Mach Memory Dump (after run):")
	buf.Reset()
	m.Dump(&buf)
	t.logLines(buf.String())

	t.checkResults(m, false)
}

func (t testCaseRun) checkResults(m *stackvm.Mach, expect bool) {
	if t.Results == nil {
		assert.Nil(t, t.res, "unexpected results")
		actual, err := t.Result.take(m)
		assert.NoError(t, err, "unexpected error taking result")
		assert.Equal(t, t.Result, actual, "expected result")
	} else if expect {
		assert.Equal(t, t.Results, t.res, "expected results")
	}
}

func (t testCaseRun) logLines(s string) {
	for _, line := range strings.Split(s, "\n") {
		t.Logf(line)
	}
}

func (t testCaseRun) build(handle func(*stackvm.Mach) error) *stackvm.Mach {
	m := stackvm.New(t.StackSize)
	require.NoError(t, m.Load(t.Prog),
		"unexpected machine compile error")
	if t.Results != nil {
		qs := t.QueueSize
		if qs <= 0 {
			qs = 10
		}
		m.SetHandler(qs, handle)
	}
	return m
}

func (t testCaseRun) checkError(err error) {
	if t.Err == nil {
		assert.NoError(t, err, "unexpected run error")
	} else {
		assert.EqualError(t, err, t.Err.Error(), "unexpected run error")
	}
}

func (t testCaseRun) checkResult(m *stackvm.Mach) error {
	var expected Result
	i := len(t.res)
	if i < len(t.Results) {
		expected = t.Results[i]
	}
	actual, err := expected.take(m)
	assert.NoError(t, err, "unexpected error taking result")
	assert.Equal(t, expected, actual, "expected result[%d]", i)
	return nil
}

func (t *testCaseRun) takeResult(m *stackvm.Mach) error {
	var res Result
	if i := len(t.res); i < len(t.Results) {
		res = t.Results[i]
	}
	res, err := res.take(m)
	assert.NoError(t, err, "unexpected error taking result")
	t.res = append(t.res, res)
	return nil
}

func (r Result) take(m *stackvm.Mach) (res Result, err error) {
	res.Err = m.Err()
	res.PS, res.CS, err = m.Stacks()
	if len(r.Mem) > 0 {
		res.Mem = make([]ResultMem, len(r.Mem))
	}
	for i := 0; i < len(r.Mem); i++ {
		addr := r.Mem[i].Addr
		data := make([]byte, len(r.Mem[i].Data))
		res.Mem[i] = ResultMem{
			Addr: addr,
			Data: data[:m.MemCopy(addr, data)],
		}
	}
	return
}
