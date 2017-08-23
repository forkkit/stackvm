package stackvm

// MachHandler is implemented to handle multiple results during a machine run;
// without a handler being set, any fork operation will fail.
type MachHandler interface {
	Handle(*Mach) error
}

// MachHandlerFunc is a convenient way to implement a simple Handler.
type MachHandlerFunc func(m *Mach) error

// Handle calls the function.
func (f MachHandlerFunc) Handle(m *Mach) error { return f(m) }

var defaultHandler MachHandler = MachHandlerFunc((*Mach).Err)
