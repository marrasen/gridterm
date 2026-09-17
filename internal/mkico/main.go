// Command mkico writes gridterm's icon as a Windows .ico file and says
// where it went.
//
// The file is not what a running window uses: that sets its own icon
// from the same drawing. It is what a Windows resource is built from, so
// the executable carries an icon in Explorer and on a pinned shortcut.
// See the icon target in the Makefile.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/marrasen/gridterm/appicon"
)

func main() {
	out := flag.String("o", "", "where to write the icon, or a temporary file when empty")
	flag.Parse()

	raw, err := appicon.ICO()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	at, err := write(*out, raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// The path, so a build step can hand it straight to the resource
	// compiler without choosing a name of its own.
	fmt.Println(at)
}

// write puts the icon at a path, or in a temporary file when there is
// none, and returns where it went.
func write(at string, raw []byte) (string, error) {
	if at != "" {
		if err := os.WriteFile(at, raw, 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", at, err)
		}
		return at, nil
	}
	f, err := os.CreateTemp("", "gridterm-*.ico")
	if err != nil {
		return "", fmt.Errorf("make a file for the icon: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		return "", fmt.Errorf("write %s: %w", f.Name(), errors.Join(err, f.Close()))
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("write %s: %w", f.Name(), err)
	}
	return f.Name(), nil
}
