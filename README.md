# Toy Stack Machine in Go

An experiment grown out of repeated solutions to [a toy integer
search/programming problem][intsearch]:
- ... where-in a [C stack machine][intcstack] solved it
- ... where-in a [Go register-architecture VM][intgoreg] solved it

The key twist is that the vm has a `FORK` instruction:
- `FORK` works like `JUMP`, except both paths are taken
- this works by copying the current machine state and:
  - having the copy `JUMP` normally
  - while the original continues on, as if it ignored the `FORK`
- as a corollary, a `BRANCH` is like `FORK`, but the original machine makes the
  `JUMP` while the copy continues

Conditional forms of `FORK` and `BRANCH` work similarly to `JUMP` wrt `JZ` and
`JNZ`:
- `FZ`/`FNZ` are fork if (non-)zero
- `BZ`/`BNZ` are branch if (non-)zero

Of course this means that we need some way of handling multiple descendant
copies while running a machine. Perhaps the simplest thing to do:
- push copies onto a queue of pending machines
- after the current machine run ends, pop and run a machine from the queue
- continue like this until the queue is empty, or an abnormal termination occurs

# Status

The VM itself and its assembler are mostly done at this point.  Next up is more
tests, performance tuning, and kicking the tires on other problems.

For bleeding edge, see the [`dev`][dev] branch.

# TODO

- yield?
- score?

- make op definitions more self-documenting
- breakup the Tracer interface:
  - Observer factors out for just lifecycle (Begin,End,Queue,Handle)
  - Tracer is an Observer with per-op observability: Before and After
- add heap range pointers
- add zigzagging to the varint arg encoder
- measure test coverage
- support for resolving halt codes to domain specific errors
- stricter memory model, including
  - page flags
  - require calling an "allocate" operation
  - would also allow shared pages
- provide some sort of static program verification; at least "can I decode it?"
- ops:
  - consolidate dispatch in Mach.step; fix latent bug around mutating invalid
    m.pa, when it should be an underflow
  - missing op to dump regs (ip, \[cp\]\[bs\]p, to (c)stack
  - forking/branching call/ret
- unsure if should add subroutine definition support to the assembler, or just
  start on a compiler
- support naming dynamic output values

[intsearch]: https://github.com/jcorbin/intsearch
[intcstack]: https://github.com/jcorbin/intsearch/tree/c_stack_machine_2015-11
[intgoreg]: https://github.com/jcorbin/intsearch/tree/go_2016-04
[dev]: (https://github.com/jcorbin/intsearch/tree/dev)
[forth]: https://en.wikipedia.org/wiki/Forth_(programming_language)
