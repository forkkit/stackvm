package stackvm

import (
	"errors"
	"fmt"
	"sync/atomic"
)

var (
	errVarIntTooBig  = errors.New("varint argument too big")
	errInvalid       = errors.New("invalid argument")
	errSegfault      = errors.New("segfault")
	errNoConetxt     = errors.New("no context, cannot copy")
	errUnimplemented = errors.New("unipmlemented")
)

var ops = [256]func(arg uint32, have bool) op{
	push, pop, dup, swap, nil, nil, nil, nil,
	neg, add, sub, mul, div, mod, divmod, nil,
	lt, lte, eq, neq, gt, gte, nil, nil,
	not, and, or, xor, nil, nil, nil, nil,
	jump, jnz, jz, nil, nil, nil, nil, nil,
	fork, fnz, fz, nil, nil, nil, nil, nil,
	branch, bnz, bz, nil, nil, nil, nil, nil,
	cpop, p2c, c2p, nil, nil, nil, nil, nil,
	call, ret, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, nil,
	nil, nil, nil, nil, nil, nil, nil, halt,
}

// Mach is a stack machine
type Mach struct {
	ctx      context
	ip       int     // next op to decode
	pbp, psp int     // param stack
	cbp, csp int     // control stack
	pages    []*page // memory
}

type context interface {
	queue(*Mach) error
}

type op func(*Mach) error

type page struct {
	r int32
	d [64]byte
}

func (m *Mach) step() error {
	ip, op, err := m.decode(m.ip)
	if err != nil {
		return err
	}
	m.ip = ip
	if err := op(m); err != nil {
		return err
	}

	return nil
}

func (m *Mach) decode(addr int) (int, op, error) {
	end := addr
	i, j, pg := m.pageFor(addr)
	arg := uint32(0)
	for k := 0; k < 5; k++ {
		end++
		for j > 0x3f {
			i, j = i+1, j-0x3f
			pg = m.page(i)
		}
		val := pg.d[j]
		if val&0x80 == 0 {
			code := val & 0x7f
			op := ops[code](arg, k > 0)
			if op == nil {
				return end, nil, decodeError{
					addr, end,
					code, k > 0, arg,
				}
			}
			return end, op, nil
		}
		j++
		if k == 4 {
			break
		}
		arg = arg<<7 | uint32(val&0x7f)
	}
	return end, nil, errVarIntTooBig
}

func (m *Mach) jump(off int) error {
	ip := m.ip + off
	if ip >= m.pbp && ip <= m.cbp {
		return errSegfault
	}
	m.ip = ip
	return nil
}

func (m *Mach) fork(off int) error {
	if m.ctx == nil {
		return errNoConetxt
	}
	ip := m.ip + off
	if ip >= m.pbp && ip <= m.cbp {
		return errSegfault
	}
	n := *m
	n.pages = n.pages[:len(n.pages):len(n.pages)]
	m.ip = ip
	return m.ctx.queue(&n)
}

func (m *Mach) branch(off int) error {
	if m.ctx == nil {
		return errNoConetxt
	}
	ip := m.ip + off
	if ip >= m.pbp && ip <= m.cbp {
		return errSegfault
	}
	n := *m
	n.pages = n.pages[:len(n.pages):len(n.pages)]
	n.ip = ip
	return m.ctx.queue(&n)
}

func (m *Mach) call(ip int) error {
	return errUnimplemented // FIXME ip int vs byte memory
	// if ip >= m.pbp && ip <= m.cbp {
	// 	return errSegfault
	// }
	// if err := m.cpush(m.ip); err != nil {
	// 	return err
	// }
	// m.ip = ip
	// return nil
}

func (m *Mach) ret() error {
	return errUnimplemented // FIXME ip int vs byte memory
	// ip, err := m.cpop()
	// if err != nil {
	// 	return err
	// }
	// m.ip = ip
	// return nil
}

func (m *Mach) fetch(addr int) byte {
	_, j, pg := m.pageFor(addr)
	return pg.d[j]
}

func (m *Mach) store(addr int, val byte) {
	i, j, pg := m.pageFor(addr)
	if r := atomic.LoadInt32(&pg.r); r > 1 {
		newPage := &page{r: 1, d: pg.d}
		m.pages[i] = newPage
		atomic.AddInt32(&pg.r, -1)
		pg = newPage
	}
	pg.d[j] = val
}

func (m *Mach) pageFor(addr int) (i, j int, pg *page) {
	i, j = addr>>6, addr&0x3f
	pg = m.page(i)
	return
}

func (m *Mach) page(i int) *page {
	if i >= len(m.pages) {
		pages := make([]*page, i+1)
		copy(pages, m.pages)
		m.pages = pages
	}
	pg := m.pages[i]
	if pg == nil {
		pg = &page{r: 1}
		m.pages[i] = pg
	}
	return pg
}

func (m *Mach) push(val byte) error {
	if m.psp < m.csp {
		m.store(m.psp, val)
		m.psp++
		return nil
	}
	return stackRangeError{"param", "over"}
}

func (m *Mach) pop() (byte, error) {
	if psp := m.psp - 1; psp >= m.pbp {
		m.psp = psp
		return m.fetch(psp), nil
	}
	return 0, stackRangeError{"param", "under"}
}

func (m *Mach) pAddr(off int) (int, error) {
	if addr := m.psp - off; addr >= m.pbp {
		return addr, nil
	}
	return 0, stackRangeError{"param", "under"}
}

func (m *Mach) cpush(val byte) error {
	if m.csp > m.psp {
		m.store(m.csp, val)
		m.csp--
		return nil
	}
	return stackRangeError{"control", "over"}
}

func (m *Mach) cpop() (byte, error) {
	if csp := m.csp + 1; csp <= m.cbp {
		m.csp = csp
		return m.fetch(csp), nil
	}
	return 0, stackRangeError{"control", "under"}
}

func (m *Mach) cAddr(off int) (int, error) {
	if addr := m.csp + off; addr <= m.cbp {
		return addr, nil
	}
	return 0, stackRangeError{"code", "under"}
}

type stackRangeError struct {
	name string
	kind string
}

func (sre stackRangeError) Error() string {
	return fmt.Sprintf("%s stack %sflow", sre.name, sre.kind)
}

type decodeError struct {
	start, end int
	code       byte
	have       bool
	arg        uint32
}

func (de decodeError) Error() string {
	return fmt.Sprintf(
		"failed to decode @%d:%d code=%d have=%v arg=%v",
		de.start, de.end, de.code, de.have, de.arg)
}