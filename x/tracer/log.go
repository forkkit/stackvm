package tracer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jcorbin/stackvm"
	"github.com/jcorbin/stackvm/internal/errors"
)

const noteWidth = 15

// NewLogTracer creates a tracer that logs machine state using a printf-style
// string "logging" function
func NewLogTracer(f func(string, ...interface{})) stackvm.Tracer {
	return logfTracer(f)
}

type logfTracer func(string, ...interface{})

func (lf logfTracer) Context(m *stackvm.Mach, key string) (interface{}, bool) {
	if key != "logf" {
		return nil, false
	}
	mid, _ := m.Tracer().Context(m, "id")
	pfx := fmt.Sprintf("%v       ... ", mid)
	return func(format string, args ...interface{}) {
		lf(pfx+format, args...)
	}, true
}

func (lf logfTracer) Begin(m *stackvm.Mach) {
	if nvs, err := m.NamedValues(); err != nil {
		lf.note(m, "===", "Begin", "values_err=%q pbp=0x%04x cbp=0x%04x", err, m.PBP(), m.CBP())
	} else {
		lf.note(m, "===", "Begin", "%s pbp=0x%04x cbp=0x%04x", namedValueParts(nvs), m.PBP(), m.CBP())
	}
}

func (lf logfTracer) End(m *stackvm.Mach) {
	if err := m.Err(); err != nil {
		lf.note(m, "===", "End", "err=%q", errors.Cause(err))
	} else if nvs, err := m.NamedValues(); err != nil {
		lf.note(m, "===", "End", "values_err=%q", err)
	} else {
		lf.note(m, "===", "End", "%s", namedValueParts(nvs))
	}
}

func (lf logfTracer) Queue(m, n *stackvm.Mach) {
	mid, _ := m.Tracer().Context(n, "id")
	if nvs, err := m.NamedValues(); err != nil {
		lf.note(m, "+++", "Copy", "values_err=%q child=%v", err, mid)
	} else {
		lf.note(m, "+++", "Copy", "%s child=%v", namedValueParts(nvs), mid)
	}
}

func (lf logfTracer) Handle(m *stackvm.Mach, err error) {
	if err != nil {
		lf.note(m, "!!!", "Handle", "err=%q", err)
	} else {
		lf.note(m, "===", "Handle")
	}
}

func (lf logfTracer) Before(m *stackvm.Mach, ip uint32, op stackvm.Op) {
	lf.note(m, ">>>", op, "%s", lf.stackAnnotation(m))
}

func (lf logfTracer) After(m *stackvm.Mach, ip uint32, op stackvm.Op) {
	lf.note(m, "...", op, "%s", lf.stackAnnotation(m))
}

func (lf logfTracer) stackAnnotation(m *stackvm.Mach) string {
	ps, cs, err := m.Stacks()
	if err != nil {
		return fmt.Sprintf(
			"pbp=0x%04x psp=0x%04x csp=0x%04x cbp=0x%04x stacks_err=%q",
			m.PBP(), m.PSP(), m.CSP(), m.CBP(), err)
	}
	return fmt.Sprintf(
		"ps=%v cs=%v psp=0x%04x csp=0x%04x",
		ps, cs, m.PSP(), m.CSP())
}

func (lf logfTracer) note(m *stackvm.Mach, mark string, note interface{}, args ...interface{}) {
	var format string
	var parts []interface{}

	mid, _ := m.Tracer().Context(m, "id")

	if count, _ := m.Tracer().Context(m, "count"); count != nil {
		format = "%v #% 4d %s % *v @0x%04x"
		parts = []interface{}{mid, count, mark, noteWidth, note, m.IP()}
	} else {
		format = "%v #% 4d %s % *v @0x%04x"
		parts = []interface{}{mid, 0, mark, noteWidth, note, m.IP()}
	}

	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			format += " " + s
			args = args[1:]
		}
		parts = append(parts, args...)
	}
	lf(format, parts...)
}

func namedValueParts(nvs map[string][]uint32) string {
	parts := make([]string, 0, len(nvs))
	for n, vs := range nvs {
		parts = append(parts, fmt.Sprintf("out_%s=%v", n, vs))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}
