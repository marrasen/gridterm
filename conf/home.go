package conf

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ExpandHome reads a path the way a shell does, with ~ for home, and
// makes it absolute, from the folder kakel runs in.
func ExpandHome(path string) (string, error) {
	path, err := Tilde(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

// Tilde reads a leading ~ as home, as a shell does, and leaves the rest
// of a path as it is: one that is not full stays so, for a caller that
// wants a full path to refuse it. On Windows, ~\ is home as ~/ is.
func Tilde(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") || runtime.GOOS == "windows" && strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return path, nil
}
