package stackvm

import "errors"

var errRunQFull = errors.New("run queue full")

// Handler is implemented to handle multiple results during a machine run;
// without a handler being set, any fork operation will fail.
type Handler interface {
	Handle(*Mach) error
}

// HandlerFunc is a conveniente way to implement a simple Handler.
type HandlerFunc func(m *Mach) error

// Handle calls the function.
func (f HandlerFunc) Handle(m *Mach) error { return f(m) }

type context interface {
	Handler
	Enqueue(*Mach) error
	Dequeue() *Mach
}

// runq implements a capped lifo queue
type runq struct {
	Handler
	q []*Mach
}

func newRunq(h Handler, n int) *runq {
	return &runq{h, make([]*Mach, 0, n)}
}

func (rq *runq) Enqueue(m *Mach) error {
	if len(rq.q) == cap(rq.q) {
		return errRunQFull
	}
	rq.q = append(rq.q, m)
	return nil
}

func (rq *runq) Dequeue() *Mach {
	if len(rq.q) == 0 {
		return nil
	}
	i := len(rq.q) - 1
	m := rq.q[i]
	rq.q = rq.q[:i]
	return m
}

var defaultContext = _defaultContext{}

type _defaultContext struct{}

func (dc _defaultContext) Handle(m *Mach) error { return m.Err() }
func (dc _defaultContext) Enqueue(*Mach) error  { return errNoQueue }
func (dc _defaultContext) Dequeue() *Mach       { return nil }
