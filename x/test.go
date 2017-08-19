package xstackvm

import (
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jcorbin/stackvm"
	"github.com/jcorbin/stackvm/internal/errors"
	"github.com/jcorbin/stackvm/x/action"
	"github.com/jcorbin/stackvm/x/dumper"
	"github.com/jcorbin/stackvm/x/tracer"
)

var (
	traceFlag    bool
	dumpProgFlag bool
	dumpMemFlag  action.PredicateFlag
)

func init() {
	flag.BoolVar(&traceFlag, "stackvm.test.trace", false,
		"run any stackvm tests with tracing on, even if they pass")
	flag.BoolVar(&dumpProgFlag, "stackvm.test.dumpprog", false,
		"dump assembled program before loading into machine")
	flag.BoolVar(&dumper.DumpPointers, "stackvm.test.dumptrs", false,
		"annotate memory dumps with pointer addresses")
	flag.Var(&dumpMemFlag, "stackvm.test.dumpmem",
		"dump memory when the given predicates are true (FIXME predicates?!?)")
}

// TestCases is list of test cases for stackvm.
type TestCases []TestCase

// TestCase is a test case for a stackvm.
type TestCase struct {
	Logf    func(format string, args ...interface{})
	Name    string
	Prog    []byte
	Err     string
	Handler func(*stackvm.Mach) ([]byte, error)
	Result  TestCaseResult
}

// TestCaseResult represents an expectation for TestCase.Results.  Both of the
// Result and Results types implement this interface, and can be used directly
// to express simple expectations.
type TestCaseResult interface {
	start(tb testing.TB, m *stackvm.Mach) finisher
}

type finisher interface {
	finish(m *stackvm.Mach)
}

type handler interface {
	Handle(m *stackvm.Mach) error
}

type testCaseRun struct {
	testing.TB
	Logf func(string, ...interface{})
	TestCase
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

// TraceTo traces each test case in a sub-test.
func (tcs TestCases) TraceTo(t *testing.T, w io.Writer) {
	for _, tc := range tcs {
		t.Run(tc.Name, tc.LogTo(w).Trace)
	}
}

type ioLogger struct {
	err error
	w   io.Writer
}

func (iol *ioLogger) logf(format string, args ...interface{}) {
	if iol.err != nil {
		return
	}
	if _, err := fmt.Fprintf(iol.w, format+"\n", args...); err != nil {
		iol.err = err
	}
}

// LogTo returns a copy of the test case with Logf
// changed to print to the given io.Writer.
func (tc TestCase) LogTo(w io.Writer) TestCase {
	iol := ioLogger{w: w}
	tc.Logf = iol.logf
	return tc
}

// Run runs the test case; it either succeeds quietly, or fails with a trace
// log.
func (tc TestCase) Run(t *testing.T) {
	run := testCaseRun{
		TB:       t,
		TestCase: tc,
	}
	watching := dumpProgFlag || traceFlag
	if watching || run.canaryFailed() {
		run.trace()
	}
}

// Bench benchmarks the test case.
func (tc TestCase) Bench(b *testing.B) {
	for i := 0; i < b.N; i++ {

		run := testCaseRun{
			TB:       b,
			TestCase: tc,
		}
		run.bench()

	}
}

// Trace runs the test case with trace logging on.
func (tc TestCase) Trace(t *testing.T) {
	run := testCaseRun{
		TB:       t,
		TestCase: tc,
	}
	run.trace()
}

func (t testCaseRun) contextLog(m *stackvm.Mach) func(string, ...interface{}) {
	logf := t.Logf
	if v, def := m.Tracer().Context(m, "logf"); def {
		if f, ok := v.(func(string, ...interface{})); ok {
			logf = f
		}
	}
	return logf
}

func (t *testCaseRun) init() {
	if t.Logf == nil {
		t.Logf = t.TB.Logf
	}
	if t.Result == nil {
		t.Result = NoResult
	}
}

func (t testCaseRun) bench() {
	t.init()
	m, err := t.build()
	require.NoError(t, err, "unexpected build error")
	fin := t.Result.start(t.TB, m)
	if h, ok := fin.(stackvm.Handler); ok {
		m.SetHandler(h)
	}
	t.checkError(m.Run())
	fin.finish(m)
}

func (t testCaseRun) canaryFailed() bool {
	t.init()
	t.TB = &testing.T{}
	m, err := t.build()
	if err != nil {
		return true
	}
	fin := t.Result.start(t.TB, m)
	if h, ok := fin.(stackvm.Handler); ok {
		m.SetHandler(h)
	}
	t.checkError(m.Run())
	fin.finish(m)
	return t.Failed()
}

func (t testCaseRun) trace() {
	t.init()
	trc := tracer.Multi(
		tracer.NewIDTracer(),
		tracer.NewCountTracer(),
		tracer.NewLogTracer(t.Logf),
		tracer.Filtered(
			tracer.FuncTracer(func(m *stackvm.Mach) {
				_ = dumper.Dump(m, t.contextLog(m))
			}),
			dumpMemFlag.Build(),
		),
	)

	m, err := t.build()
	require.NoError(t, err, "unexpected build error")
	fin := t.Result.start(t.TB, m)
	if h, ok := fin.(stackvm.Handler); ok {
		m.SetHandler(h)
	}
	t.checkError(m.Trace(trc))
	fin.finish(m)
}

func (t testCaseRun) logLines(s string) {
	for _, line := range strings.Split(s, "\n") {
		t.Logf(line)
	}
}

func (t testCaseRun) build() (*stackvm.Mach, error) {
	if dumpProgFlag {
		// TODO: reconcile with stackvm/x/dumper
		t.Logf("Program to Load:")
		t.logLines(hex.Dump(t.Prog))
	}
	return stackvm.New(t.Prog)
}

func (t testCaseRun) checkError(err error) {
	if t.Err == "" {
		assert.NoError(t, err, "unexpected run error")
	} else {
		assert.EqualError(t, errors.Cause(err), t.Err, "expected run error")
	}
}

// NoResult is a TestCaseResult that expects no results from any number of
// machine under a test.
var NoResult = _NoResult{}

type _NoResult struct{}
type noResult struct{ testing.TB }

func (nr _NoResult) start(tb testing.TB, m *stackvm.Mach) finisher { return noResult{tb} }

// WithExpectedHaltCodes creates a TestCaseResult that expects any number of
// non-zero halt codes in addition to expecting no result values. If a machine
// exits with an unexpected non-zero halt code, the test still fails.
func (nr _NoResult) WithExpectedHaltCodes(codes ...uint32) TestCaseResult {
	return filteredResults{nr, []resultChecker{expectedHaltCodes(codes)}}
}

func (nr noResult) Handle(m *stackvm.Mach) error {
	res, err := Result{}.take(m)
	if err != nil {
		return err
	}
	assert.Equal(nr, Result{}, res, "expected empty result")
	return nil
}

func (nr noResult) finish(m *stackvm.Mach) {
	if m != nil {
		res, err := Result{}.take(m)
		if assert.NoError(nr, err, "unexpected error taking final result") {
			nr.result(res)
		}
	}
}

func (nr noResult) result(res Result) {
	assert.Equal(nr, Result{}, res, "expected empty result")
}

// Result represents an expected or actual result within a TestCase. It can be
// used as a TestCaseResult when only a single final result is expected.
type Result struct {
	Err    string
	Values [][]uint32
}

func (r Result) take(m *stackvm.Mach) (res Result, err error) {
	if merr := m.Err(); merr != nil {
		res.Err = errors.Cause(merr).Error()
	} else {
		res.Values, err = m.Values()
	}
	return
}

func (r Result) start(tb testing.TB, m *stackvm.Mach) finisher { return &runResult{tb, r, false} }

type runResult struct {
	testing.TB
	Result
	got bool
}

func (rr *runResult) finish(m *stackvm.Mach) {
	if rr.got {
		assert.Nil(rr, m, "expected singular result")
		return
	}
	if assert.NotNil(rr, m, "must have a final machine") {
		actual, err := rr.Result.take(m)
		if assert.NoError(rr, err, "unexpected error taking final result") {
			rr.result(actual)
		}
	}
}

func (rr *runResult) result(actual Result) {
	if assert.False(rr, rr.got, "expected singular result") {
		assert.Equal(rr, rr.Result, actual, "expected result")
		rr.got = true
	}
}

// Results represents multiple expected results. It can be used as a
// TestCaseResult to require an exact sequence of results, including failed
// non-zero halt states.
type Results []Result

func (rs Results) start(tb testing.TB, m *stackvm.Mach) finisher {
	return &runResults{tb, rs, 0}
}

// WithExpectedHaltCodes creates a TestCaseResult that expects any number of
// non-zero halt codes in addition to some normal results. If a machine exits
// with an unexpected non-zero halt code, the test still fails.
func (rs Results) WithExpectedHaltCodes(codes ...uint32) TestCaseResult {
	return filteredResults{rs, []resultChecker{expectedHaltCodes(codes)}}
}

type expectedHaltCodes []uint32

func (codes expectedHaltCodes) check(tb testing.TB, m *stackvm.Mach) bool {
	// NOTE we don't get 0 because that's mapped to nil by Mach.Err
	// TODO maybe context should define expected codes, and just be mapped
	// to zero by mach.Err?
	if code, ok := m.HaltCode(); ok && code != 0 {
		for i := range codes {
			if code == codes[i] {
				return true
			}
		}
		assert.Fail(tb, "unexpected halt code", "got %d, expected one of %d", code, codes)
		return true
	}
	return false
}

type resultChecker interface {
	check(tb testing.TB, m *stackvm.Mach) bool
}

type filteredResults struct {
	TestCaseResult
	cs []resultChecker
}

func (frs filteredResults) start(tb testing.TB, m *stackvm.Mach) finisher {
	fin := frs.TestCaseResult.start(tb, m)
	return newFilteredRunResults(tb, fin, frs.cs)
}

type runResults struct {
	testing.TB
	expected Results
	i        int
}

func (rrs *runResults) Handle(m *stackvm.Mach) error {
	var expected Result
	i := rrs.i
	if i < len(rrs.expected) {
		expected = rrs.expected[i]
	}
	if err := m.Err(); err != nil && expected.Err == "" {
		if _, halted := m.HaltCode(); !halted {
			return err
		}
	}
	actual, err := expected.take(m)
	rrs.i++
	if err != nil {
		return err
	}
	if i >= len(rrs.expected) {
		assert.Fail(rrs, "unexpected result", "unexpected result[%d]: %+v", i, actual)
	} else {
		assert.Equal(rrs, expected, actual, "expected result[%d]", i)
	}
	return nil
}

func (rrs *runResults) finish(m *stackvm.Mach) {
	n := len(rrs.expected)
	switch {
	case rrs.i == 0:
		assert.Fail(rrs, "no results", "got %d of %d result(s)", rrs.i, n)
	case rrs.i < n:
		assert.Fail(rrs, "not enough results", "got %d of %d result(s)", rrs.i, n)
	}
}

type filteredRunResults struct {
	testing.TB
	finisher
	handler
	cs []resultChecker
}

func newFilteredRunResults(tb testing.TB, fin finisher, cs []resultChecker) *filteredRunResults {
	hndl, _ := fin.(handler)
	return &filteredRunResults{
		TB:       tb,
		finisher: fin,
		handler:  hndl,
		cs:       cs,
	}
}

func (frrs *filteredRunResults) Handle(m *stackvm.Mach) error {
	for _, c := range frrs.cs {
		if c.check(frrs.TB, m) {
			return nil
		}
	}
	if frrs.handler != nil {
		return frrs.handler.Handle(m)
	}
	return nil
}

func (frrs *filteredRunResults) finish(m *stackvm.Mach) {
	for _, c := range frrs.cs {
		if c.check(frrs.TB, m) {
			frrs.finisher.finish(nil)
			return
		}
	}
	frrs.finisher.finish(m)
}
