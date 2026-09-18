//go:build !windows

package main

// platformLetGos is empty away from Windows: the ordinary syscall
// numbers cover what a socket returns there.
func platformLetGos() []letGo { return nil }
