//go:build windows && amd64

package conpty

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// bundleVersion is the version of the ConPTY package the files come
// from; see openconsole/README.md.
const bundleVersion = "1.25.260930003"

var (
	//go:embed openconsole/conpty.dll
	conptyDLL []byte
	//go:embed openconsole/OpenConsole.exe
	openConsole []byte
)

// bundled puts OpenConsole where it can run and loads it. conpty.dll
// starts OpenConsole.exe from beside itself. Files already there are
// used if they are the ones kakel carries, so a second kakel does not
// write over files the first has loaded.
//
// They go in the user's temporary folder. They are only a copy of what
// kakel carries, and put back if they go. A loaded DLL cannot be removed
// until the process ends, so it stays out of kakel's own folder, which
// a portable copy keeps beside itself and a test makes and removes.
func bundled() (*host, error) { return bundledIn(placeDir()) }

// bundledIn puts OpenConsole in dir and loads it from there.
func bundledIn(dir string) (*host, error) {
	if err := placeAll(dir); err != nil {
		return nil, err
	}
	d := windows.NewLazyDLL(filepath.Join(dir, "conpty.dll"))
	h := &host{
		name:   "OpenConsole " + bundleVersion,
		create: d.NewProc("ConptyCreatePseudoConsole"),
		resize: d.NewProc("ConptyResizePseudoConsole"),
		close:  d.NewProc("ConptyClosePseudoConsole"),
		ready:  func() error { return placeAll(dir) },
	}
	for _, p := range []*windows.LazyProc{h.create, h.resize, h.close} {
		if err := p.Find(); err != nil {
			return nil, fmt.Errorf("load OpenConsole: %w", err)
		}
	}
	return h, nil
}

// placeDir is where OpenConsole is put.
func placeDir() string { return filepath.Join(os.TempDir(), "kakel-conpty-"+bundleVersion) }

// placeAll puts conpty.dll and OpenConsole.exe in dir.
func placeAll(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("make a place for OpenConsole: %w", err)
	}
	if err := place(filepath.Join(dir, "conpty.dll"), conptyDLL); err != nil {
		return err
	}
	return place(filepath.Join(dir, "OpenConsole.exe"), openConsole)
}

// place makes path hold data, unless it already does. It writes beside
// it and renames, so a file is never there half written.
func place(path string, data []byte) error {
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, data) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".placing-*")
	if err != nil {
		return fmt.Errorf("put %s in place: %w", filepath.Base(path), err)
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		// Another kakel may have put it there meanwhile, and be running
		// it, which stops the rename.
		if have, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(have, data) {
			return nil
		}
		return fmt.Errorf("put %s in place: %w", filepath.Base(path), err)
	}
	return nil
}
