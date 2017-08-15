package stackvm

// Handler is implemented to handle multiple results during a machine run;
// without a handler being set, any fork operation will fail.
type Handler interface {
	Handle(*Mach) error
}

// HandlerFunc is a conveniente way to implement a simple Handler.
type HandlerFunc func(m *Mach) error

// Handle calls the function.
func (f HandlerFunc) Handle(m *Mach) error { return f(m) }

var defaultHandler Handler = HandlerFunc((*Mach).Err)
