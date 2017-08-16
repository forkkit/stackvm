package stackvm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

var (
	errRunning = errors.New("machine running")
	errNoArg   = errors.New("operation does not accept an argument")
	errVarOpts = errors.New("truncated options")
)

// NoSuchOpError is returned by ResolveOp if the named operation is not //
// defined.
type NoSuchOpError string

func (name NoSuchOpError) Error() string {
	return fmt.Sprintf("no such operation %q", string(name))
}

// New creates a new stack machine with a given program loaded. The prog byte
// array is a sequence of varint encoded unsigned integers (after fixed encoded
// options).
//
// The first fixed byte is a version number, which must currently be 0x00.
//
// The next two bytes encode a 16-bit unsigned stacksize. That much space will
// be reserved in memory for the Parameter Stack (PS) and Control Stack (CS);
// it must be a multiple of the page size.
//
// PS grows up from 0, the PS Base Pointer PBP, to at most stacksize bytes. CS
// grows down from stacksize-1, the CS Base Pointer CBP, towards PS. The
// address of the next slot for PS (resp CS) is stored in the PS Stack Pointer,
// or PSP (resp CSP).
//
// Any push onto either PS or CS will fail with an overflow error when PSP ==
// CSP. Similarly any pop from them will fail with an underflow error when
// their SP meets their BP.
//
// The rest of prog is loaded in memory immediately after the stack space with
// IP pointing at its first byte. Each varint encodes an operation, with the
// lowest 7 bits being the opcode, while all higher bits may encode an
// immediate argument.
//
// For many non-control flow operations, any immediate argument is used in lieu
// of popping a value from the parameter stack. Most control flow operations
// use their immediate argument as an IP offset, however they will consume an
// IP offset from the parameter stack if no immediate is given.
func New(prog []byte) (*Mach, error) {
	p := prog

	opts, n, err := readMachOptions(p)
	if err != nil {
		return nil, err
	}
	p = p[n:]

	m := Mach{
		opc:   makeOpCache(len(p)),
		pbp:   0,
		psp:   _pspInit,
		cbp:   uint32(opts.StackSize) - 4,
		csp:   uint32(opts.StackSize) - 4,
		ip:    uint32(opts.StackSize),
		limit: uint(opts.MaxOps),
	}

	m.init()
	m.storeBytes(m.ip, p)
	// TODO mark code segment, update data

	return &m, nil
}

func (m *Mach) String() string {
	var buf bytes.Buffer
	buf.WriteString("Mach")
	if m.err != nil {
		if code, halted := m.halted(); halted {
			// TODO: symbolicate
			fmt.Fprintf(&buf, " HALT:%v", code)
		} else {
			fmt.Fprintf(&buf, " ERR:%v", m.err)
		}
	}
	fmt.Fprintf(&buf, " @0x%04x 0x%04x:0x%04x 0x%04x:0x%04x", m.ip, m.pbp, m.psp, m.cbp, m.csp)
	// TODO:
	// pages?
	// stack dump?
	// context describe?
	return buf.String()
}

// EachPage calls a function with each allocated section of memory; it MUST NOT
// mutate the memory, and should copy out any data that it needs to retain.
func (m *Mach) EachPage(f func(addr uint32, p *[_pageSize]byte) error) error {
	for i, pg := range m.pages {
		if pg != nil {
			if err := f(uint32(i*_pageSize), &pg.d); err != nil {
				return err
			}
		}
	}
	return nil
}

var zeroPageData [_pageSize]byte

// WriteTo writes all machine memory to the given io.Writer, returning the
// number of bytes written.
func (m *Mach) WriteTo(w io.Writer) (n int64, err error) {
	for _, pg := range m.pages {
		var wn int
		if pg == nil {
			wn, err = w.Write(zeroPageData[:])
		} else {
			wn, err = w.Write(pg.d[:])
		}
		n += int64(wn)
		if err != nil {
			break
		}
	}
	return
}

// IP returns the current instruction pointer.
func (m *Mach) IP() uint32 { return m.ip }

// PBP returns the current parameter stack base pointer.
func (m *Mach) PBP() uint32 { return m.pbp }

// PSP returns the current parameter stack pointer.
func (m *Mach) PSP() uint32 {
	if m.psp > m.cbp {
		return m.pbp
	}
	return m.psp
}

// CBP returns the current control stack base pointer.
func (m *Mach) CBP() uint32 { return m.cbp }

// CSP returns the current control stack pointer.
func (m *Mach) CSP() uint32 { return m.csp }

// Values returns any recorded result values from a finished machine. After a
// machine halts with 0 status code, the control stack may contain zero or
// more pairs of memory address ranges. If so, then Values will extract all
// such ranged values, and return them as a slice-of-slices.
func (m *Mach) Values() ([][]uint32, error) {
	if m.err == nil {
		return nil, errRunning
	}

	if arg, ok := m.halted(); !ok || arg != 0 {
		if m.err != nil {
			return nil, m.err
		}
		return nil, errRunning
	}

	cs, err := m.fetchCS()
	if err != nil {
		return nil, err
	}
	if len(cs)%2 != 0 {
		return nil, fmt.Errorf("invalid control stack length %d", len(cs))
	}
	if len(cs) == 0 {
		return nil, nil
	}

	res := make([][]uint32, 0, len(cs)/2)
	for i := 0; i < len(cs); i += 2 {
		ns, err := m.fetchMany(cs[i], cs[i+1])
		if err != nil {
			return nil, err
		}
		res = append(res, ns)
	}
	return res, nil
}

// Stacks returns the current values on the parameter and control
// stacks.
func (m *Mach) Stacks() ([]uint32, []uint32, error) {
	ps, err := m.fetchPS()
	if err != nil {
		return nil, nil, err
	}
	cs, err := m.fetchCS()
	if err != nil {
		return nil, nil, err
	}
	return ps, cs, nil
}

// MemCopy copies bytes from memory into the given buffer, returning
// the number of bytes copied.
func (m *Mach) MemCopy(addr uint32, bs []byte) int {
	return m.fetchBytes(addr, bs)
}

// Tracer is the interface taken by (*Mach).Trace to observe machine
// execution: Begin() and End() are called when a machine starts and finishes
// respectively; Before() and After() are around each machine operation;
// Queue() is called when a machine creates a copy of itself; Handle() is
// called after an ended machine has been passed to any result handling
// function.
//
// Contextual information may be made available by implementing the Context()
// method: if a tracer wants defines a value for some key, it should return
// that value and a true boolean. Tracers, and other code, may then use
// (*Mach).Tracer().Context() to access contextual information from other
// tracers.
type Tracer interface {
	Context(m *Mach, key string) (interface{}, bool)
	Begin(m *Mach)
	Before(m *Mach, ip uint32, op Op)
	After(m *Mach, ip uint32, op Op)
	Queue(m, n *Mach)
	End(m *Mach)
	Handle(m *Mach, err error)
}

// Op is used within Tracer to pass along decoded machine operations.
type Op struct {
	Code byte
	Arg  uint32
	Have bool
}

// ResolveOp builds an op given a name string, and argument.
func ResolveOp(name string, arg uint32, have bool) (Op, error) {
	code, def := opName2Code[name]
	if !def {
		return Op{}, NoSuchOpError(name)
	}
	if have && ops[code].imm.kind() == opImmNone {
		return Op{}, errNoArg
	}
	return Op{code, arg, have}, nil
}

// Name returns the name of the coded operation.
func (o Op) Name() string {
	return ops[o.Code].name
}

const (
	optCodeVersion uint8 = iota
	optCodeStackSize
	optCodeMaxOps
	optCodeEnd = 0x7f
)

// MachOptions represents options for a machine, currently just stack size (see
// New).
type MachOptions struct {
	StackSize uint16
	MaxOps    uint32
}

func readMachOptions(buf []byte) (opts MachOptions, n int, err error) {
	for {
		m, arg, code, ok := readVarCode(buf[n:])
		n += m
		if !ok {
			err = errVarOpts
			return
		}
		switch code {

		case optCodeVersion:
		case 0x80 | optCodeVersion:
			if arg != 0 {
				err = fmt.Errorf("unsupported machine version %v", arg)
				return
			}

		case 0x80 | optCodeStackSize:
			if arg > 0xffff {
				err = fmt.Errorf("invalid stacksize %#x", arg)
				return
			}
			if arg%4 != 0 {
				err = fmt.Errorf("invalid stacksize %#02x, not a word-multiple", arg)
				return
			}
			opts.StackSize = uint16(arg)

		case optCodeMaxOps:
			opts.MaxOps = 0

		case 0x80 | optCodeMaxOps:
			opts.MaxOps = arg

		case optCodeEnd:
			return

		default:
			err = fmt.Errorf("invalid option code %#02x", code)
			return
		}
	}
}

// EncodeInto encodes machine optios for the header of a program.
func (opts MachOptions) EncodeInto(p []byte) (n int) {
	n += putVarCode(p[n:], 0, optCodeVersion)
	if opts.StackSize != 0 {
		n += putVarCode(p[n:], uint32(opts.StackSize), 0x80|optCodeStackSize)
	}
	if opts.MaxOps != 0 {
		n += putVarCode(p[n:], opts.MaxOps, 0x80|optCodeMaxOps)
	}
	n += putVarCode(p[n:], 0, optCodeEnd)
	return
}

// NeededSize returns the number of bytes needed for EncodeInto.
func (opts MachOptions) NeededSize() (n int) {
	n += varCodeLength(0, optCodeVersion)
	if opts.StackSize != 0 {
		n += varCodeLength(uint32(opts.StackSize), 0x80|optCodeStackSize)
	}
	if opts.MaxOps != 0 {
		n += varCodeLength(opts.MaxOps, 0x80|optCodeMaxOps)
	}
	n += varCodeLength(0, optCodeEnd)
	return
}

// EncodeInto encodes the operation into the given buffer, returning the number
// of bytes encoded.
func (o Op) EncodeInto(p []byte) int {
	c := uint8(o.Code)
	if o.Have {
		c |= 0x80
	}
	return putVarCode(p, o.Arg, c)
}

// NeededSize returns the number of bytes needed to encode op.
func (o Op) NeededSize() int {
	c := uint8(o.Code)
	if o.Have {
		c |= 0x80
	}
	return varCodeLength(o.Arg, c)
}

// AcceptsRef return true only if the argument can resolve another op reference
// ala ResolveRefArg.
func (o Op) AcceptsRef() bool {
	switch ops[o.Code].imm.kind() {
	case opImmVal, opImmOffset, opImmAddr:
		return true
	}
	return false
}

// ResolveRefArg fills in the argument of a control op relative to another op's
// encoded location, and the current op's.
func (o Op) ResolveRefArg(myIP, targIP uint32) Op {
	switch ops[o.Code].imm.kind() {
	case opImmOffset:
		// need to skip the arg and the code...
		c := uint8(o.Code)
		if o.Have {
			c |= 0x80
		}

		d := targIP - myIP
		n := varCodeLength(d, c)
		d -= uint32(n)
		if id := int32(d); id < 0 && varCodeLength(uint32(id), c) != n {
			// ...arg off by one, now that we know its value.
			id--
			d = uint32(id)
		}
		o.Arg = d

	case opImmVal, opImmAddr:
		o.Arg = targIP
	}
	return o
}

func (o Op) String() string {
	def := ops[o.Code]
	if !o.Have {
		return def.name
	}
	switch def.imm.kind() {
	case opImmVal:
		return fmt.Sprintf("%d %s", o.Arg, def.name)
	case opImmAddr:
		return fmt.Sprintf("@%#04x %s", o.Arg, def.name)
	case opImmOffset:
		return fmt.Sprintf("%+#04x %s", o.Arg, def.name)
	}
	return fmt.Sprintf("INVALID(%#x %x %q)", o.Arg, o.Code, def.name)
}

// Tracer returns the current Tracer that the machine is running under, if any.
func (m *Mach) Tracer() Tracer {
	mt1, ok1 := m.ctx.Handler.(*machTracer)
	mt2, ok2 := m.ctx.queue.(*machTracer)
	if !ok1 && !ok2 {
		return nil
	}
	if !ok1 || !ok2 || mt1 != mt2 {
		panic("broken machTracer setup")
	}
	return mt1.t
}

type machTracer struct {
	Handler
	queue
	t Tracer
	m *Mach
}

func fixTracer(t Tracer, m *Mach) {
	h := m.ctx.Handler
	for mt, ok := h.(*machTracer); ok; mt, ok = h.(*machTracer) {
		h = mt.Handler
	}
	q := m.ctx.queue
	for mt, ok := q.(*machTracer); ok; mt, ok = q.(*machTracer) {
		q = mt.queue
	}
	mt := &machTracer{h, q, t, m}
	m.ctx.Handler = mt
	m.ctx.queue = mt
}

const defaultQueueSize = 10

// SetHandler sets a result handling function. Without a result handling
// function, there's not much point to running more than one machine. If no
// queue size has been set, a default 10-sized queue will be setup.
func (m *Mach) SetHandler(h Handler) {
	m.ctx.Handler = h
	if m.ctx.queue == nil ||
		m.ctx.queue == noQueue {
		m.SetQueueSize(defaultQueueSize)
	}
}

// SetQueueSize sets up a non-thread safe queue to support forking and
// branching. Without a queue, the fork and branch instructions will fail. If
// no machine or page allocators have yet been created, a couple freelist
// allocators are created with initial capacities hinted from the queue size.
func (m *Mach) SetQueueSize(n int) {
	const pagesPerMachineGuess = 4
	m.ctx.queue = newRunq(n)
	if m.ctx.machAllocator == nil ||
		m.ctx.machAllocator == defaultMachAllocator {
		m.ctx.machAllocator = makeMachFreeList(n)
	}
	if m.ctx.pageAllocator == nil ||
		m.ctx.pageAllocator == defaultPageAllocator {
		m.ctx.pageAllocator = makePageFreeList(n * pagesPerMachineGuess)
	}
}

func (mt *machTracer) Enqueue(n *Mach) error {
	mt.t.Queue(mt.m, n)
	fixTracer(mt.t, n)
	return mt.queue.Enqueue(n)
}

// Trace implements the same logic as (*Mach).run, but calls a Tracer
// at the appropriate times.
func (m *Mach) Trace(t Tracer) error {
	// the code below is essentially an
	// instrumented copy of Mach.Run (with mach.run
	// inlined)

	orig := m

	fixTracer(t, m)

repeat:
	// live
	t.Begin(m)
	for m.err == nil {
		var readOp Op
		if _, code, arg, err := m.read(m.ip); err != nil {
			m.err = err
			break
		} else {
			readOp = Op{code.code(), arg, code.hasImm()}
		}
		t.Before(m, m.ip, readOp)
		m.step()
		if m.err != nil {
			break
		}
		t.After(m, m.ip, readOp)
	}
	t.End(m)

	// win or die
	err := m.ctx.Handle(m)
	t.Handle(m, err)
	if err == nil {
		if n := m.ctx.Dequeue(); n != nil {
			m.free()
			m = n
			// die
			goto repeat
		}
	}

	// win?
	if m != orig {
		*orig = *m
	}
	return err
}

// Run runs the machine until termination, returning any error.
func (m *Mach) Run() error {
	n, err := m.run()
	if n != m {
		*m = *n
	}
	return err
}

// Step single steps the machine; it decodes and executes one
// operation.
func (m *Mach) Step() error {
	if m.err == nil {
		m.step()
	}
	return m.Err()
}

// HaltCode returns the halt code and true if the machine has halted
// normally; otherwise false is returned.
func (m *Mach) HaltCode() (uint32, bool) { return m.halted() }

var (
	lowHaltErrors [256]error
	haltErrors    = make(map[uint32]error)
)

func init() {
	for i := 0; i < len(lowHaltErrors); i++ {
		lowHaltErrors[i] = fmt.Errorf("HALT(%d)", i)
	}
}

// Err returns the last error from machine execution, wrapped with
// execution context.
func (m *Mach) Err() error {
	err := m.err
	if code, halted := m.halted(); halted {
		if code == 0 {
			return nil
		}
		if code < uint32(len(lowHaltErrors)) {
			err = lowHaltErrors[code]
		} else {
			he, def := haltErrors[code]
			if !def {
				he = fmt.Errorf("HALT(%d)", code)
				haltErrors[code] = he
			}
			err = he
		}
	}
	if err == nil {
		return nil
	}
	if _, ok := err.(MachError); !ok {
		return MachError{m.ip, err}
	}
	return err
}

// MachError wraps an underlying machine error with machine state.
type MachError struct {
	addr uint32
	err  error
}

// Cause returns the underlying machine error.
func (me MachError) Cause() error { return me.err }

func (me MachError) Error() string { return fmt.Sprintf("@0x%04x: %v", me.addr, me.err) }
