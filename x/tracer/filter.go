package tracer

import (
	"github.com/jcorbin/stackvm"
	"github.com/jcorbin/stackvm/x/action"
)

// Filtered returns a tracer that calls the given tracer's methods only if the
// given predicate tests true. Context simply passes through.
func Filtered(t stackvm.Tracer, p action.Predicate) stackvm.Tracer {
	switch p {
	case nil:
		return nil
	case action.Never:
		return nil
	case action.Always:
		return t
	default:
		return filter{t, p}
	}
}

type filter struct {
	stackvm.Tracer
	action.Predicate
}

func (f filter) Begin(m *stackvm.Mach) {
	if f.Test(action.TraceBegin, 0, stackvm.Op{}) {
		f.Tracer.Begin(m)
	}
}

func (f filter) End(m *stackvm.Mach) {
	if f.Test(action.TraceEnd, 0, stackvm.Op{}) {
		f.Tracer.End(m)
	}
}

func (f filter) Before(m *stackvm.Mach, ip uint32, op stackvm.Op) {
	if f.Test(action.TraceBefore, ip, op) {
		f.Tracer.Before(m, ip, op)
	}
}

func (f filter) After(m *stackvm.Mach, ip uint32, op stackvm.Op) {
	if f.Test(action.TraceAfter, ip, op) {
		f.Tracer.After(m, ip, op)
	}
}

func (f filter) Queue(m, n *stackvm.Mach) {
	if f.Test(action.TraceQueue, 0, stackvm.Op{}) {
		f.Tracer.Queue(m, n)
	}
}

func (f filter) Handle(m *stackvm.Mach, err error) {
	if f.Test(action.TraceHandle, 0, stackvm.Op{}) {
		f.Tracer.Handle(m, err)
	}
}
