package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/ui"
)

// filesCommand is the command that says where gridterm keeps its files,
// and filesTitle names both the notice and the line that opens it.
const (
	filesCommand = "help.files"
	filesTitle   = "File Locations"
)

// showWhereFiles says which directory gridterm is reading and writing,
// and how to make a copy carry files of its own.
func (a *app) showWhereFiles() error {
	body, err := whereFilesText()
	if err != nil {
		return err
	}
	n := a.newNotice(filesTitle, body)
	// A path is not prose, and the dialog would re-wrap one at a space.
	n.Preformatted = true

	// And a button that does what the message describes, for a copy
	// that is not carrying its own files yet.
	own, beside, err := conf.CarriesItsOwn()
	if err != nil {
		return err
	}
	// And not once the directory is there. Where this window reads from
	// was decided when it opened and does not move, so it goes on saying
	// it is not carrying its own files until it is started again --
	// which is no reason to offer to make a directory that now exists.
	made, err := isDir(beside)
	if err != nil {
		return err
	}
	if !own && !made {
		n.Action = ui.NoticeAction{Title: carryOwnTitle, Do: a.startCarryingOwnFiles}
		n.FocusOK()
	}
	a.presentNotice(n)
	return nil
}

// startCarryingOwnFiles does what the notice describes and says how it
// went, which is the button's whole job.
func (a *app) startCarryingOwnFiles() {
	said, err := a.carryOwnFiles()
	if err != nil {
		a.reportError("Could not make portable", err)
		return
	}
	n := a.newNotice(carriedOwnTitle, said)
	// A line per thing done, which the dialog would otherwise run
	// together into a paragraph.
	n.Preformatted = true
	a.presentNotice(n)
}

// whereFilesText is what the notice says, for this copy of gridterm.
func whereFilesText() (string, error) {
	dir, err := conf.Dir()
	if err != nil {
		return "", err
	}
	private, err := serve.HostKeyPath()
	if err != nil {
		return "", err
	}
	own, beside, err := conf.CarriesItsOwn()
	if err != nil {
		return "", err
	}
	return filesSay(where{dir: dir, hostKey: private, own: own, beside: beside}), nil
}

// where is the place gridterm keeps what it remembers.
type where struct {
	// dir holds everything except the host key, which is private and on
	// Windows lives off the roaming profile until gridterm carries its
	// own files.
	dir     string
	hostKey string

	// own says the files are beside this copy of gridterm, and beside is
	// that directory whether they are there or not.
	own    bool
	beside string
}

// filesSay writes the notice out: a line per file, then either what a
// copy carrying its own should know or how to make one.
func filesSay(f where) string {
	var b strings.Builder
	for _, line := range []struct{ what, name string }{
		{"Settings", settings.File},
		{"Saved servers", remote.BookFile},
		{"Themes", themes.File},
		{"Shortcuts", keys.File},
		{"Authorized keys", serve.AuthFile},
		{"Known windows", knownWindowsFile},
	} {
		fmt.Fprintf(&b, "%-18s%s\n", line.what+":", filepath.Join(f.dir, line.name))
	}
	fmt.Fprintf(&b, "%-18s%s\n", "Serving key:", f.hostKey)
	b.WriteString("\nSSH keys and known_hosts stay in ~/.ssh.\n")

	if f.own {
		// The three steps are the button's job now, so what is left is
		// the one thing a portable copy has to know: where its files are.
		fmt.Fprintf(&b, "\nPortable: files are kept beside gridterm, in\n\n    %s\n\n"+
			"The serving key is in there too, and is only as private as\n"+
			"that directory.\n", f.beside)
	}
	return b.String()
}
