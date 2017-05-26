package stackvm

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	_pageSize        = 0x40
	_pageMask        = _pageSize - 1
	_machVersionCode = 0x00
	_pspInit         = 0xfffffffc
)

var (
	errVarIntTooBig = errors.New("varint argument too big")
	errInvalidIP    = errors.New("invalid IP")
	errSegfault     = errors.New("segfault")
	errNoQueue      = errors.New("no queue, cannot copy")
	errAlignment    = errors.New("unaligned memory access")
	errHalted       = errors.New("halted")
)

type alignmentError struct {
	op   string
	addr uint32
}

func (ae alignmentError) Error() string {
	return fmt.Sprintf("unaligned memory %s @0x%04x", ae.op, ae.addr)
}

// Mach is a stack machine.
type Mach struct {
	ctx      context // execution context
	opc      opCache // op decode cache
	err      error   // non-nil after termination
	ip       uint32  // next op to decode
	pbp, psp uint32  // param stack
	pa       uint32  // param head
	cbp, csp uint32  // control stack
	// TODO track code segment and data segment
	pages []*page // memory
}

func makeOpCache(n int) opCache {
	return opCache{
		cos: make([]cachedOp, n),
	}
}

type opCache struct {
	cos []cachedOp
}

func (opc opCache) get(k uint32) (co cachedOp, ok bool) {
	if k < uint32(len(opc.cos)) && opc.cos[k].ip != 0 {
		co, ok = opc.cos[k], true
	}
	return
}

func (opc *opCache) set(k uint32, co cachedOp) {
	if k >= uint32(len(opc.cos)) {
		cos := make([]cachedOp, k+1)
		copy(cos, opc.cos)
		opc.cos = cos
	}
	opc.cos[k] = co
	return
}

type cachedOp struct {
	ip   uint32
	code opCode
	arg  uint32
}

type page struct {
	r int32
	d [_pageSize]byte
}

func (pg *page) fetchByte(off uint32) byte {
	if pg == nil {
		return 0
	}
	return pg.d[off]
}

func (pg *page) fetch(off uint32) (uint32, error) {
	if off%4 != 0 {
		return 0, errAlignment
	}
	if pg == nil {
		return 0, nil
	}
	val := *(*uint32)(unsafe.Pointer(&(pg.d[off])))
	return val, nil
}

var (
	machPool = sync.Pool{New: func() interface{} { return &Mach{} }}
	pagePool = sync.Pool{New: func() interface{} { return &page{r: 0} }}
)

func (pg *page) own() *page {
	if pg == nil {
		pg = pagePool.Get().(*page)
		pg.r = 1
	} else if atomic.LoadInt32(&pg.r) > 1 {
		newPage := pagePool.Get().(*page)
		newPage.r = 1
		newPage.d = pg.d
		atomic.AddInt32(&pg.r, -1)
		pg = newPage
	}
	return pg
}

func (pg *page) storeBytes(off uint32, p []byte) (*page, int) {
	pg = pg.own()
	n := copy(pg.d[off:], p)
	return pg, n
}

func (pg *page) storeByte(off uint32, val byte) *page {
	pg = pg.own()
	pg.d[off] = val
	return pg
}

func (pg *page) ref(off uint32) (*page, *uint32, error) {
	if off%4 != 0 {
		return nil, nil, errAlignment
	}
	pg = pg.own()
	p := (*uint32)(unsafe.Pointer(&(pg.d[off])))
	return pg, p, nil
}

func (m *Mach) halted() (uint32, bool) {
	if m.err == errHalted {
		return m.pa, true
	}
	return 0, false
}

func (m *Mach) run() (*Mach, error) {

repeat:
	// live
	for m.err == nil {
		m.step()
	}

	// win or die
	err := m.ctx.Handle(m)
	if err == nil {
		if n := m.ctx.next(); n != nil {
			m.free()
			m = n
			// die
			goto repeat
		}
	}

	// win?
	return m, err
}

func (m *Mach) step() {
	// decode
	ck := m.ip - m.cbp
	oc, cached := m.opc.get(ck)
	if !cached {
		oc.ip, oc.code, oc.arg, m.err = m.read(m.ip)
		if m.err != nil {
			return
		}
		m.opc.set(ck, oc)
	}
	m.ip = oc.ip

	// execute
	switch oc.code {
	// stack
	case opCodePush | opCodeWithImm:
		m.err = m.push(oc.arg)

	case opCodePop:
		m.err = m.drop()
	case opCodePop | opCodeWithImm:
		switch oc.arg {
		case 1:
			m.err = m.drop()
		default:
			for i := uint32(0); i < oc.arg && m.err == nil; i++ {
				m.err = m.drop()
			}
		}

	case opCodeDup:
		m.err = m.push(m.pa)
	case opCodeDup | opCodeWithImm:
		switch oc.arg {
		case 1:
			m.err = m.push(m.pa)
		default:
			p, err := m.pRef(oc.arg)
			if err == nil {
				err = m.push(*p)
			}
			m.err = err
		}

	case opCodeSwap:
		if m.psp == _pspInit {
			m.err = stackRangeError{"param", "under"}
			return
		}
		p, err := m.pRef(2)
		if err == nil {
			m.pa, *p = *p, m.pa
		}
		m.err = err

	case opCodeSwap | opCodeWithImm:
		if m.psp == _pspInit {
			m.err = stackRangeError{"param", "under"}
			return
		}
		p, err := m.pRef(1 + oc.arg)
		if err == nil {
			m.pa, *p = *p, m.pa
		}
		m.err = err

	// memory
	case opCodeFetch:
		addr, err := m.pop()
		if err == nil {
			val, err := m.fetch(addr)
			if err == nil {
				err = m.push(val)
			}
		}
		m.err = err

	case opCodeStore:
		addr, err := m.pop()
		if err == nil {
			var val uint32
			val, err = m.pop()
			if err == nil {
				err = m.store(addr, val)
			}
		}
		m.err = err

	case opCodeFetch | opCodeWithImm:
		val, err := m.fetch(oc.arg)
		if err == nil {
			err = m.push(val)
		}
		m.err = err

	case opCodeStore | opCodeWithImm:
		val, err := m.pop()
		if err == nil {
			err = m.store(oc.arg, val)
		}
		m.err = err

	// math
	case opCodeNeg:
		m.pa = -m.pa

	case opCodeAdd:
		b, err := m.pop()
		if err == nil {
			m.pa += b
		}
		m.err = err
	case opCodeSub:
		b, err := m.pop()
		if err == nil {
			m.pa -= b
		}
		m.err = err
	case opCodeMul:
		b, err := m.pop()
		if err == nil {
			m.pa *= b
		}
		m.err = err
	case opCodeDiv:
		b, err := m.pop()
		if err == nil {
			m.pa /= b
		}
		m.err = err
	case opCodeMod:
		b, err := m.pop()
		if err == nil {
			m.pa = uint32(rem(int32(m.pa), int32(b)))
		}
		m.err = err
	case opCodeDivmod:
		bp, err := m.pRef(2)
		if err == nil {
			b := *bp
			v := m.pa
			m.pa = v / b
			*bp = uint32(rem(int32(v), int32(b)))
		}
		m.err = err

	case opCodeAdd | opCodeWithImm:
		m.pa += oc.arg
	case opCodeSub | opCodeWithImm:
		m.pa -= oc.arg
	case opCodeMul | opCodeWithImm:
		m.pa *= oc.arg
	case opCodeDiv | opCodeWithImm:
		m.pa /= oc.arg
	case opCodeMod | opCodeWithImm:
		m.pa = uint32(rem(int32(m.pa), int32(oc.arg)))
	case opCodeDivmod | opCodeWithImm:
		v := m.pa
		m.pa = v / oc.arg
		m.err = m.push(uint32(rem(int32(m.pa), int32(oc.arg))))

	// boolean logic
	case opCodeLt:
		b, err := m.pop()
		if err == nil {
			m.pa = bool2uint32(m.pa < b)
		}
		m.err = err
	case opCodeLte:
		b, err := m.pop()
		if err == nil {
			m.pa = bool2uint32(m.pa <= b)
		}
		m.err = err
	case opCodeEq:
		b, err := m.pop()
		if err == nil {
			m.pa = bool2uint32(m.pa == b)
		}
		m.err = err
	case opCodeNeq:
		b, err := m.pop()
		if err == nil {
			m.pa = bool2uint32(m.pa != b)
		}
		m.err = err
	case opCodeGt:
		b, err := m.pop()
		if err == nil {
			m.pa = bool2uint32(m.pa > b)
		}
		m.err = err
	case opCodeGte:
		b, err := m.pop()
		if err != nil {
			m.pa = bool2uint32(m.pa >= b)
		}
		m.err = err
	case opCodeNot:
		m.pa = bool2uint32(m.pa == 0)
	case opCodeAnd:
		b, err := m.pop()
		if err != nil {
			m.pa = bool2uint32((m.pa != 0) && (b != 0))
		}
		m.err = err
	case opCodeOr:
		b, err := m.pop()
		if err != nil {
			m.pa = bool2uint32((m.pa != 0) || (b != 0))
		}
		m.err = err

	case opCodeLt | opCodeWithImm:
		m.pa = bool2uint32(m.pa < oc.arg)
	case opCodeLte | opCodeWithImm:
		m.pa = bool2uint32(m.pa <= oc.arg)
	case opCodeEq | opCodeWithImm:
		m.pa = bool2uint32(m.pa == oc.arg)
	case opCodeNeq | opCodeWithImm:
		m.pa = bool2uint32(m.pa != oc.arg)
	case opCodeGt | opCodeWithImm:
		m.pa = bool2uint32(m.pa > oc.arg)
	case opCodeGte | opCodeWithImm:
		m.pa = bool2uint32(m.pa >= oc.arg)

	// control stack
	case opCodeMark:
		m.err = m.cpush(m.ip)
	case opCodeCpop:
		_, m.err = m.cpop()
	case opCodeP2c:
		val, err := m.pop()
		if err == nil {
			err = m.cpush(val)
		}
		m.err = err
	case opCodeC2p:
		val, err := m.cpop()
		if err == nil {
			err = m.push(val)
		}
		m.err = err
	case opCodeCpush | opCodeWithImm:
		m.err = m.cpush(oc.arg)
	case opCodeCpop | opCodeWithImm:
		for i := uint32(0); i < oc.arg && m.err == nil; i++ {
			_, m.err = m.cpop()
		}
	case opCodeP2c | opCodeWithImm:
		for i := uint32(0); i < oc.arg && m.err == nil; i++ {
			val, err := m.pop()
			if err == nil {
				err = m.cpush(val)
			}
			m.err = err
		}
	case opCodeC2p | opCodeWithImm:
		for i := uint32(0); i < oc.arg && m.err == nil; i++ {
			val, err := m.cpop()
			if err == nil {
				err = m.push(val)
			}
			m.err = err
		}

	// control flow: jumps
	case opCodeJump:
		val, err := m.pop()
		if err == nil {
			err = m.jump(int32(val))
		}
		m.err = err
	case opCodeJnz:
		val, err := m.pop()
		if err == nil && val != 0 {
			err = m.cjump()
		}
		m.err = err
	case opCodeJz:
		val, err := m.pop()
		if err == nil && val == 0 {
			err = m.cjump()
		}
		m.err = err
	case opCodeJump | opCodeWithImm:
		m.err = m.jump(int32(oc.arg))
	case opCodeJnz | opCodeWithImm:
		val, err := m.pop()
		if err == nil && val != 0 {
			err = m.jump(int32(oc.arg))
		}
		m.err = err
	case opCodeJz | opCodeWithImm:
		val, err := m.pop()
		if err == nil && val == 0 {
			m.err = m.jump(int32(oc.arg))
		}
		m.err = err

	// control flow: loops
	case opCodeLoop:
		p, err := m.cRef(0)
		if err == nil {
			err = m.jumpTo(*p)
		}
		m.err = err

	case opCodeLnz:
		p, err := m.cRef(0)
		if err == nil {
			val, e2 := m.pop()
			if e2 == nil {
				if val != 0 {
					e2 = m.jumpTo(*p)
				} else {
					e2 = m.cdrop()
				}
			}
			err = e2
		}
		m.err = err
	case opCodeLz:
		p, err := m.cRef(0)
		if err == nil {
			val, e2 := m.pop()
			if e2 == nil {
				if val == 0 {
					e2 = m.jumpTo(*p)
				} else {
					e2 = m.cdrop()
				}
			}
			err = e2
		}
		m.err = err

	// control flow: calls
	case opCodeCall:
		val, err := m.pop()
		if err == nil {
			err = m.call(val)
		}
		m.err = err
	case opCodeRet:
		m.err = m.ret()
	case opCodeCall | opCodeWithImm:
		m.err = m.call(oc.arg)

	// control: forking
	case opCodeFork:
		val, err := m.pop()
		if err == nil {
			err = m.fork(int32(val))
		}
		m.err = err
	case opCodeFnz:
		val, err := m.pop()
		if err == nil && val != 0 {
			err = m.cfork()
		}
		m.err = err
	case opCodeFz:
		val, err := m.pop()
		if err == nil && val == 0 {
			err = m.cfork()
		}
		m.err = err
	case opCodeFork | opCodeWithImm:
		m.err = m.fork(int32(oc.arg))
	case opCodeFnz | opCodeWithImm:
		val, err := m.pop()
		if err == nil && val != 0 {
			err = m.fork(int32(oc.arg))
		}
		m.err = err
	case opCodeFz | opCodeWithImm:
		val, err := m.pop()
		if err == nil && val == 0 {
			err = m.fork(int32(oc.arg))
		}
		m.err = err

	// control: branching
	case opCodeBranch:
		val, err := m.pop()
		if err == nil {
			err = m.branch(int32(val))
		}
		m.err = err
	case opCodeBnz:
		val, err := m.pop()
		if err != nil && val != 0 {
			err = m.cbranch()
		}
		m.err = err
	case opCodeBz:
		val, err := m.pop()
		if err == nil && val == 0 {
			err = m.cbranch()
		}
		m.err = err
	case opCodeBranch | opCodeWithImm:
		m.err = m.branch(int32(oc.arg))
	case opCodeBnz | opCodeWithImm:
		val, err := m.pop()
		if err == nil && val != 0 {
			err = m.branch(int32(oc.arg))
		}
		m.err = err
	case opCodeBz | opCodeWithImm:
		val, err := m.pop()
		if err == nil && val == 0 {
			err = m.branch(int32(oc.arg))
		}
		m.err = err

	// control: halt
	case opCodeHalt, opCodeHalt | opCodeWithImm:
		m.pa = oc.arg
		m.err = errHalted
	case opCodeHz, opCodeHz | opCodeWithImm:
		val, err := m.pop()
		if err == nil && val == 0 {
			m.pa = oc.arg
			err = errHalted
		}
		m.err = err
	case opCodeHnz, opCodeHnz | opCodeWithImm:
		val, err := m.pop()
		if err == nil && val != 0 {
			m.pa = oc.arg
			err = errHalted
		}
		m.err = err

	}
}

func (m *Mach) read(addr uint32) (end uint32, code opCode, arg uint32, err error) {
	var bs [6]byte
	end = addr
	n := m.fetchBytes(addr, bs[:])
	for k := 0; k < n; k++ {
		val := bs[k]
		end++
		if val&0x80 == 0 {
			code = opCode(val)
			if k > 0 {
				code |= opCodeWithImm
			}
			goto validate
		}
		if k == len(bs)-1 {
			break
		}
		arg = arg<<7 | uint32(val&0x7f)
	}
	if n < len(bs) {
		err = errInvalidIP
	} else {
		err = errVarIntTooBig
	}
	return

validate:

	have := code.hasImm()
	def := ops[code.code()]
	if def.name == "" {
		if have {
			err = fmt.Errorf("invalid op code:%#02x arg:%#08x", code, arg)
		} else {
			err = fmt.Errorf("invalid op code:%#02x", code)
		}
		return
	}

	if have && def.imm.kind() == opImmNone {
		err = fmt.Errorf("unexpected immediate argument %#04x for %q op", arg, def.name)
		return
	}

	if !have && def.imm.required() {
		err = fmt.Errorf("missing immediate argument for %q op", def.name)
		return
	}

	return
}

func (m *Mach) jump(off int32) error {
	return m.jumpTo(uint32(int32(m.ip) + off))
}

func (m *Mach) cjump() error {
	ip, err := m.cpop()
	if err != nil {
		return err
	}
	return m.jumpTo(ip)
}

func (m *Mach) jumpTo(ip uint32) error {
	if ip >= m.pbp && ip <= m.cbp {
		return errSegfault
	}
	m.ip = ip
	return nil
}

func (m *Mach) copy() (*Mach, error) {
	n := machPool.Get().(*Mach)
	pgs := n.pages
	*n = *m
	if cap(pgs) < len(m.pages) {
		pgs = make([]*page, 0, len(m.pages))
	}
	pgs = pgs[:len(m.pages)]
	for i, pg := range m.pages {
		if pg != nil {
			pgs[i] = pg
			atomic.AddInt32(&pg.r, 1)
		}
	}
	n.pages = pgs
	return n, nil
}

func (m *Mach) free() {
	for i, pg := range m.pages {
		if pg != nil {
			if atomic.AddInt32(&pg.r, -1) <= 0 {
				pagePool.Put(pg)
			}
		}
		m.pages[i] = nil
	}
	m.pages = m.pages[:0]
	machPool.Put(m)
}

func (m *Mach) fork(off int32) error {
	ip := uint32(int32(m.ip) + off)
	if ip >= m.pbp && ip <= m.cbp {
		return errSegfault
	}
	n, err := m.copy()
	if err != nil {
		return err
	}
	n.ip = ip
	return m.ctx.queue(n)
}

func (m *Mach) cfork() error {
	n, err := m.copy()
	if err != nil {
		return err
	}
	ip, err := n.cpop()
	if err != nil {
		return err
	}
	if err := n.jumpTo(ip); err != nil {
		return err
	}
	return m.ctx.queue(n)
}

func (m *Mach) branch(off int32) error {
	ip := uint32(int32(m.ip) + off)
	if ip >= m.pbp && ip <= m.cbp {
		return errSegfault
	}
	n, err := m.copy()
	if err != nil {
		return err
	}
	m.ip = ip
	return m.ctx.queue(n)
}

func (m *Mach) cbranch() error {
	n, err := m.copy()
	if err != nil {
		return err
	}
	ip, err := m.cpop()
	if err != nil {
		return err
	}
	if err := m.ctx.queue(n); err != nil {
		return err
	}
	return m.jumpTo(ip)
}

func (m *Mach) loop() error {
	p, err := m.cRef(0)
	if err != nil {
		return err
	}
	return m.jumpTo(*p)
}

func (m *Mach) call(ip uint32) error {
	if ip >= m.pbp && ip <= m.cbp {
		return errSegfault
	}
	if err := m.cpush(m.ip); err != nil {
		return err
	}
	return m.jumpTo(ip)
}

func (m *Mach) ret() error {
	ip, err := m.cpop()
	if err != nil {
		return err
	}
	return m.jumpTo(ip)
}

func (m *Mach) fetchPS() ([]uint32, error) {
	psp := m.psp
	if psp == _pspInit {
		return nil, nil
	}
	if psp == 0 {
		return []uint32{m.pa}, nil
	}
	var vals []uint32
	if psp < _pspInit {
		if psp > m.cbp {
			return nil, stackRangeError{"param", "under"}
		}
		if psp > m.csp {
			return nil, stackRangeError{"param", "over"}
		}
		if psp > 0 {
			vs, err := m.fetchMany(m.pbp, psp)
			if err != nil {
				return nil, err
			}
			vals = vs
		}
	}
	vals = append(vals, m.pa)
	return vals, nil
}

func (m *Mach) fetchCS() ([]uint32, error) {
	csp := m.csp
	if csp == m.cbp {
		return nil, nil
	}
	if csp < m.psp && m.psp < m.cbp {
		return nil, stackRangeError{"control", "over"}
	}
	if csp > m.cbp {
		return nil, stackRangeError{"control", "under"}
	}
	return m.fetchMany(m.cbp, csp)
}

func (m *Mach) fetchMany(from, to uint32) ([]uint32, error) {
	if from == to {
		return nil, nil
	}

	if from < to {
		ns := make([]uint32, 0, (to-from)/4)
		for ; from < to; from += 4 {
			val, err := m.fetch(from)
			if err != nil {
				return nil, err
			}
			ns = append(ns, val)
		}
		return ns, nil
	}

	// to < from
	ns := make([]uint32, 0, (from-to)/4)
	for ; from > to; from -= 4 {
		val, err := m.fetch(from)
		if err != nil {
			return nil, err
		}
		ns = append(ns, val)
	}
	return ns, nil
}

func (m *Mach) fetchBytes(addr uint32, bs []byte) (n int) {
	var pg *page
	i, j := addr>>6, addr&_pageMask
	if int(i) < len(m.pages) {
		pg = m.pages[i]
	}
	for n < len(bs) {
		if j > _pageMask {
			i++
			j &= _pageMask
			if int(i) < len(m.pages) {
				pg = m.pages[i]
			} else {
				pg = nil
			}
		}
		if pg == nil {
			left := len(pg.d) - int(j) + 1
			if rem := len(bs) - n; rem <= left {
				n += rem
				break
			}
			j += uint32(left)
			n += left
			continue
		}
		bs[n] = pg.d[j]
		j++
		n++
	}
	return
}

func (m *Mach) storeBytes(addr uint32, bs []byte) {
	n := 0
	var pg *page
	i, j := addr>>6, addr&_pageMask
	if int(i) < len(m.pages) {
		pg = m.pages[i]
	}

	goto doCopy

nextPage:
	i++
	j = 0
	if int(i) < len(m.pages) {
		pg = m.pages[i]
	} else {
		pg = nil
	}

doCopy:
	npg, pgn := pg.storeBytes(j, bs[n:])
	n += pgn
	if npg != pg {
		pg = m.setPage(i, npg)
	}
	if n < len(bs) {
		goto nextPage
	}
}

func (m *Mach) fetch(addr uint32) (uint32, error) {
	i, off := addr>>6, addr&_pageMask
	if off%4 != 0 {
		return 0, alignmentError{"fetch", addr}
	}
	if int(i) < len(m.pages) {
		if pg := m.pages[i]; pg != nil {
			val := *(*uint32)(unsafe.Pointer(&(pg.d[off])))
			return val, nil
		}
	}
	return 0, nil
}

func (m *Mach) ref(addr uint32) (*uint32, error) {
	i, off := addr>>6, addr&_pageMask
	if off%4 != 0 {
		return nil, alignmentError{"store", addr}
	}

	var pg *page
	if int(i) < len(m.pages) {
		pg = m.pages[i]
		if pg == nil {
			pg = pagePool.Get().(*page)
		} else if atomic.LoadInt32(&pg.r) > 1 {
			newPage := pagePool.Get().(*page)
			newPage.d = pg.d
			atomic.AddInt32(&pg.r, -1)
			pg = newPage
		} else {
			goto load
		}
	} else {
		pages := make([]*page, i+1)
		copy(pages, m.pages)
		m.pages = pages
		pg = pagePool.Get().(*page)
	}

	pg.r = 1
	m.pages[i] = pg

load:
	p := (*uint32)(unsafe.Pointer(&(pg.d[off])))
	return p, nil
}

func (m *Mach) store(addr, val uint32) error {
	p, err := m.ref(addr)
	if err == nil {
		*p = val
	}
	return err
}

func (m *Mach) setPage(i uint32, pg *page) *page {
	if int(i) >= len(m.pages) {
		pages := make([]*page, i+1)
		copy(pages, m.pages)
		m.pages = pages
	}
	m.pages[i] = pg
	return pg
}

func (m *Mach) move(src, dst uint32) error {
	val, err := m.fetch(src)
	if err != nil {
		return err
	}
	return m.store(dst, val)
}

func (m *Mach) push(val uint32) error {
	psp := m.psp + 4
	if psp < _pspInit {
		if psp > m.cbp {
			return stackRangeError{"param", "under"}
		}
		if psp > m.csp {
			return stackRangeError{"param", "over"}
		}
	}
	if psp > 0 {
		if err := m.store(m.psp, m.pa); err != nil {
			return err
		}
	}
	m.pa = val
	m.psp = psp
	return nil
}

func (m *Mach) pRef(i uint32) (*uint32, error) {
	if i == 1 {
		if m.psp == _pspInit {
			return nil, stackRangeError{"param", "under"}
		}
		return &m.pa, nil
	}
	addr := m.psp + 4 - i*4
	if addr < m.pbp || addr > m.csp {
		return nil, stackRangeError{"param", "under"}
	}
	return m.ref(addr)
}

func (m *Mach) pop() (uint32, error) {
	val := m.pa
	return val, m.drop()
}

func (m *Mach) drop() error {
	psp := m.psp - 4
	if psp < m.cbp {
		next, err := m.fetch(psp)
		if err != nil {
			return err
		}
		m.pa = next
	} else if psp < _pspInit {
		return stackRangeError{"param", "under"}
	}
	m.psp = psp
	return nil
}

func (m *Mach) cpush(val uint32) error {
	csp := m.csp - 4
	if m.psp < m.cbp && csp < m.psp {
		return stackRangeError{"control", "over"}
	}
	if err := m.store(m.csp, val); err != nil {
		return err
	}
	m.csp = csp
	return nil
}

func (m *Mach) cpop() (uint32, error) {
	if m.csp >= m.cbp {
		return 0, stackRangeError{"control", "under"}
	}
	csp := m.csp + 4
	m.csp = csp
	return m.fetch(csp)
}

func (m *Mach) cdrop() error {
	if csp := m.csp + 4; csp <= m.cbp {
		m.csp = csp
		return nil
	}
	return stackRangeError{"control", "under"}
}

func (m *Mach) cRef(i uint32) (*uint32, error) {
	addr := m.csp + i*4
	if addr > m.cbp || (m.psp > 0 && addr < m.psp) {
		return nil, stackRangeError{"code", "under"}
	}
	return m.ref(addr)
}

type stackRangeError struct {
	name string
	kind string
}

func (sre stackRangeError) Error() string {
	return fmt.Sprintf("%s stack %sflow", sre.name, sre.kind)
}

func rem(a, b int32) int32 {
	x := a % b
	if x < 0 {
		x += b
	}
	return x
}

func bool2uint32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}
