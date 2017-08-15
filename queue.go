package stackvm

import "errors"

var errRunQFull = errors.New("run queue full")

type queue interface {
	Enqueue(*Mach) error
	Dequeue() *Mach
}

// runq implements a capped lifo queue; it is not thread safe.
type runq struct {
	q []*Mach
}

func newRunq(n int) *runq {
	return &runq{make([]*Mach, 0, n)}
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

var noQueue queue = _noQueue{}

type _noQueue struct{}

func (nq _noQueue) Enqueue(*Mach) error { return errNoQueue }
func (nq _noQueue) Dequeue() *Mach      { return nil }
