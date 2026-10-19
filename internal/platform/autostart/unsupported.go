package autostart

// Unsupported is a desktop with no login start that Boku can register.
//
// It exists so that the rest of the application has one code path: every
// platform answers the same four questions, and this is the one whose answer
// to all of them is ErrUnsupported. A caller that treats an error as "the
// switch is off" is wrong only if it ignored this error; the settings page
// checks for this one before it draws anything.
type Unsupported struct{}

// Read reports that there is no login start on this desktop.
func (Unsupported) Read(Launcher) (bool, error) { return false, ErrUnsupported }

// Document reports that there is nothing to write.
func (Unsupported) Document(Launcher) (Document, error) { return Document{}, ErrUnsupported }

// Start refuses: there is nothing this desktop can start.
func (Unsupported) Start(Launcher) error { return ErrUnsupported }

// Stop refuses for the same reason: nothing was ever started from here.
func (Unsupported) Stop(Launcher) error { return ErrUnsupported }
