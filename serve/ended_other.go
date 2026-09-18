//go:build !windows

package serve

// platformEnded reports whether an error means the peer has gone, for
// the errors only one platform has. Everywhere but Windows the ordinary
// syscall numbers cover it.
func platformEnded(error) bool { return false }
