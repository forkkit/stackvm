package stackvm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

var (
	errNoArg   = errors.New("operation does not accept an argument")
	errVarOpts = errors.New("truncated options")
)

// NoSuchOpError is returned by ResolveOp if the named operation is not //
// defined.
type NoSuchOpError string

func (name NoSuchOpError) Error() string {
	return fmt.Sprintf("no such operation %q", string(name))
}

var defaultContext = context{
	Handler:       defaultHandler,
	queue:         noQueue,
	machAllocator: defaultMachAllocator,
	pageAllocator: defaultPageAllocator,
}

// New creates a new stack machine with a given program loaded. It takes a
// varcoded (more on that below) program, and an optional handler.
//
// When a non-nil handler is given, a queue is setup to handle copies of the
// machine at runtime. This handler will be called with each one after it has
// halted (explicitly, crashed, or due to an error). Without a queue, machine
// copy operations will fail (such as fork and branch).
//
// The "varcode" encoding scheme used is a variation on a varint:
// - the final byte of the varint (the one without the high bit set) encodes a
//   7-bit code-word
// - prior bytes (with their high bits set) encode an associated uint32 value
//
// This scheme is used first to encode options used to setup the machine, and
// then to encode the program that the machine will run.
//
// Valid option codes:
// - 0x00 end: indicates the end of options (beginning of program); must not
//   have a parameter.
// - 0x01 stack size: its required parameter declares the amount of memory
//   given to the parameter and control stacks (see below for details). The
//   size must be a multiple of 4 (32-bit word size). Default: 0x40.
// - 0x02 queue size: its required parameter specifies a maximum limit on how
//   many machine copies may be queued. Once this limit is reached operations
//   like fork and branch fail with queue-full error. Default: 10.
// - 0x03 max ops: its optional parameter declares a limit on the number of
//   program operations that can be executed by a single machine (the runtime
//   operation count is not shared between machine copies).
// - 0x04 max copies: its optional parameter declares a limit on the number
//   of machine copies that may be made in total. Well behaved programs
//   shouldn't need to specify this option, it should be mostly used for
//   debugging. Default: 0.
// - 0x05 entry: its required parameter is the value for IP instead of
//   starting execution at the top of the loaded program (right after the
//   stack).
// - 0x7f version: reserved for future use, where its parameter will be the
//   required machine/program version; passing a version value is currently
//   unsupported.
//
// The stack space, declared by above option or 0x40 default, is shared by the
// Parameter Stack (PS) and Control Stack (CS) which grow towards each other:
// - PS grows up from PBP=0 (PS Base Pointer) to at most stacksize bytes
// - CS grows down from CBP=stacksize-1 (CS Base Pointer) towards PS
// - the head of PS is stored in PSP (Parameter Stack Pointer)
// - the head of CS is stored in CSP (Control Stack Pointer)
// - if PSP and CSP would touch a stack overflow error occurs (reported against
//   which ever stack tried to push a value)
// - similarly an undeflow will occur if PSP would go under PBP (negative)
// - likewise an undeflow happens if CSP would go over CBP
//
// The rest of prog is loaded in memory immediately after the stack space.
// Except for data sections, prog contains varcoded operations. Each operation
// has a 7-bit opcode, and an optional 32-bit immediate value. Most operations
// treat their immediate as an optional alternative to popping a value from the
// parameter stack.
//
// The Instruction Pointer (IP) is initialized to point at the first byte after
// the stack space (0x40 by default). Machine execution then happens (under
// .Run or .Step) by decoding a varcoded operation at IP, and executing it. If
// IP becomes corrupted (points to arbitrary memory), the machine will most
// likely crash explicitly (since memory defaults to 0-filled, and the 0 opcode
// is "crash") or halt with a decode error.
//
// TODO: document operations.
func New(prog []byte, h Handler) (*Mach, error) {
	var mb machBuilder
	if err := mb.build(prog, h); err != nil {
		return nil, err
	}
	return &mb.Mach, nil
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

// Values returns any output values from the machine. Output values may be
// statically declared via the output option. Additionally, once the machine
// has halted with 0 status code, 0 or more pairs of output ranges may be left
// on the control stack.
func (m *Mach) Values() ([][]uint32, error) {
	done := false
	if m.err != nil {
		if arg, ok := m.halted(); !ok || arg != 0 {
			return nil, m.err
		}
		done = true
	}

	outputs := m.ctx.outputs
	if done {
		cs, err := m.fetchCS()
		if err != nil {
			return nil, err
		}
		if len(cs) > 0 {
			if len(cs)%2 != 0 {
				return nil, fmt.Errorf("invalid control stack length %d", len(cs))
			}
			outputs = append(make([]region, 0, len(outputs)+len(cs)/2), outputs...)
			for i := 0; i < len(cs); i += 2 {
				outputs = append(outputs, region{cs[i], cs[i+1]})
			}
		}
	}

	if len(outputs) == 0 {
		return nil, nil
	}

	res := make([][]uint32, 0, len(outputs))
	for _, rg := range outputs {
		ns, err := m.fetchMany(rg)
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

// Generates part of the New() documentation from the inline docs below.
//go:generate python collect_docs.py -i api.go -o api.go optCode "^// Valid option codes:" "^//$"

const (
	// indicates the end of options (beginning of program); must not have a
	// parameter.
	optCodeEnd uint8 = 0x00

	// its required parameter declares the amount of memory given to the
	// parameter and control stacks (see below for details). The size must be a
	// multiple of 4 (32-bit word size). Default: 0x40.
	optCodeStackSize = 0x01

	// its required parameter specifies a maximum limit on how many machine
	// copies may be queued. Once this limit is reached operations like fork
	// and branch fail with queue-full error. Default: 10.
	optCodeQueueSize = 0x02

	// its optional parameter declares a limit on the number of program
	// operations that can be executed by a single machine (the runtime
	// operation count is not shared between machine copies).
	optCodeMaxOps = 0x03

	// its optional parameter declares a limit on the number of machine copies
	// that may be made in total. Well behaved programs shouldn't need to
	// specify this option, it should be mostly used for debugging. Default: 0.
	optCodeMaxCopies = 0x04

	// its required parameter is the value for IP instead of starting execution
	// at the top of the loaded program (right after the stack).
	optCodeEntry = 0x05

	// its required parameter is an endpoint of an output range; must appear
	// in start/end pairs.
	optCodeOutput = 0x06

	// reserved for future use, where its parameter will be the required
	// machine/program version; passing a version value is currently
	// unsupported.
	optCodeVersion = 0x7f
)

type machBuilder struct {
	Mach
	base      uint32
	queueSize int
	maxCopies int

	buf []byte
	h   Handler
	n   int
}

func (mb *machBuilder) build(buf []byte, h Handler) error {
	mb.queueSize = defaultQueueSize

	mb.Mach.ctx = defaultContext
	mb.Mach.psp = _pspInit

	mb.buf = buf
	mb.h = h

	if err := mb.handleOpts(); err != nil {
		return err
	}

	return mb.finish()
}

func (mb *machBuilder) handleOpts() error {
	for {
		code, arg, err := mb.readOptCode()
		if err != nil {
			return err
		}
		if done, err := mb.handleOpt(code, arg); err != nil {
			return err
		} else if done {
			return nil
		}
	}
}

func (mb *machBuilder) readOptCode() (uint8, uint32, error) {
	n, arg, code, ok := readVarCode(mb.buf[mb.n:])
	mb.n += n
	if !ok {
		return 0, 0, errVarOpts
	}
	return code, arg, nil
}

func (mb *machBuilder) handleOpt(code uint8, arg uint32) (bool, error) {
	switch code {

	case optCodeVersion:
	case 0x80 | optCodeVersion:
		if arg != 0 {
			return false, fmt.Errorf("unsupported machine version %v", arg)
		}

	case 0x80 | optCodeStackSize:
		if arg > 0xffff {
			return false, fmt.Errorf("invalid stacksize %#x", arg)
		}
		if arg%4 != 0 {
			return false, fmt.Errorf("invalid stacksize %#02x, not a word-multiple", arg)
		}
		oldBase := mb.Mach.cbp + 4
		mb.base = uint32(arg)
		if mb.base > 0 {
			mb.Mach.cbp = mb.base - 4
			mb.Mach.csp = mb.base - 4
		}
		// TODO: else support 0
		if mb.Mach.ip == 0 || mb.Mach.ip == oldBase {
			mb.Mach.ip = mb.base
		}

	case 0x80 | optCodeQueueSize:
		mb.queueSize = int(arg)

	case optCodeMaxOps:
		mb.Mach.limit = 0

	case 0x80 | optCodeMaxOps:
		mb.Mach.limit = uint(arg)

	case optCodeMaxCopies:
		mb.maxCopies = 0

	case 0x80 | optCodeMaxCopies:
		mb.maxCopies = int(arg)

	case 0x80 | optCodeEntry:
		mb.Mach.ip = arg

	case 0x80 | optCodeOutput:
		start := arg
		code, end, err := mb.readOptCode()
		if err != nil {
			return false, err
		}
		if code != 0x80|optCodeOutput {
			return false, fmt.Errorf("unpaired output opt code, got %#02x instead", code)
		}
		mb.Mach.ctx.outputs = append(mb.Mach.ctx.outputs, region{start, end})

	case optCodeEnd:
		return true, nil

	default:
		return false, fmt.Errorf("invalid option code=%#02x have=%v arg=%#x", code&0x7f, code&0x80 != 0, arg)
	}

	return false, nil
}

func (mb *machBuilder) finish() error {
	if mb.h != nil {
		const pagesPerMachineGuess = 4
		n := int(mb.queueSize)
		mb.Mach.ctx.Handler = mb.h
		mb.Mach.ctx.queue = newRunq(n)
		mb.Mach.ctx.machAllocator = makeMachFreeList(n)
		mb.Mach.ctx.pageAllocator = makePageFreeList(n * pagesPerMachineGuess)
		if mb.maxCopies > 0 {
			mb.Mach.ctx.machAllocator = maxMachCopiesAllocator(mb.maxCopies, mb.Mach.ctx.machAllocator)
		}
	}

	prog := mb.buf[mb.n:]
	mb.Mach.opc = makeOpCache(len(prog))
	mb.Mach.storeBytes(mb.base, prog)
	// TODO mark code segment, update data

	return nil
}

// ResolveOption constructs an option Op.
func ResolveOption(name string, arg uint32, have bool) (op Op) {
	switch name {
	case "end":
		op.Code = optCodeEnd
	case "stackSize":
		op.Code = optCodeStackSize
	case "queueSize":
		op.Code = optCodeQueueSize
	case "maxOps":
		op.Code = optCodeMaxOps
	case "maxCopies":
		op.Code = optCodeMaxCopies
	case "entry":
		op.Code = optCodeEntry
	case "output":
		op.Code = optCodeOutput
	case "version":
		op.Code = optCodeVersion
	default:
		return
	}
	op.Arg = arg
	op.Have = have
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
