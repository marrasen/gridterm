//go:build !windows && !linux

package install

// Not done on this system yet.
const (
	caseless   = false
	movesAside = false
)

// Exe says installing is not done here.
func Exe() (string, error) { return "", ErrUnsupported }

func register(string, Options) error { return ErrUnsupported }

// SetAutostart says installing is not done here.
func SetAutostart(bool) error { return ErrUnsupported }

// Desktop reports false: there is no shortcut here.
func Desktop() bool { return false }

// Autostart reports false: nothing starts kakel here.
func Autostart() bool { return false }

// Uninstall says installing is not done here.
func Uninstall() error { return ErrUnsupported }
