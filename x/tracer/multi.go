package tracer

import "github.com/jcorbin/stackvm"

// Multi returns a tracer that calls each of the given tracers in sequence.
// Handle() is propagated in reverse order. Context() returns the
// first result with a true flag.
func Multi(ts ...stackvm.Tracer) stackvm.Tracer {
	switch len(ts) {
	case 0:
		return nil
	case 1:
		return ts[0]
	}
	var trs tracers
	for _, t := range ts {
		if t == nil {
			continue
		}
		if prior, ok := t.(tracers); ok {
			trs = append(trs, prior...)
		} else {
			trs = append(trs, t)
		}
	}
	return trs
}

type tracers []stackvm.Tracer

func (ts tracers) Context(m *stackvm.Mach, key string) (interface{}, bool) {
	for i := range ts {
		if val, def := ts[i].Context(m, key); def {
			return val, def
		}
	}
	return nil, false
}

func (ts tracers) Begin(m *stackvm.Mach) {
	for i := range ts {
		ts[i].Begin(m)
	}
}

func (ts tracers) Before(m *stackvm.Mach, ip uint32, op stackvm.Op) {
	for i := range ts {
		ts[i].Before(m, ip, op)
	}
}

func (ts tracers) After(m *stackvm.Mach, ip uint32, op stackvm.Op) {
	for i := range ts {
		ts[i].After(m, ip, op)
	}
}

func (ts tracers) Queue(m, n *stackvm.Mach) {
	for i := range ts {
		ts[i].Queue(m, n)
	}
}

func (ts tracers) End(m *stackvm.Mach) {
	for i := range ts {
		ts[i].End(m)
	}
}

func (ts tracers) Handle(m *stackvm.Mach, err error) {
	for i := len(ts) - 1; i >= 0; i-- {
		ts[i].Handle(m, err)
	}
}
