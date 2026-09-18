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
	filesTitle   = "Where gridterm keeps its files"
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
		a.reportError("This copy could not be given files of its own", err)
		return
	}
	a.showNotice(carryOwnTitle, said, false)
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
		{"Colour schemes", themes.File},
		{"Keyboard shortcuts", keys.File},
		{"Keys allowed to take this window over", serve.AuthFile},
		{"Windows this one has worked in", knownWindowsFile},
	} {
		fmt.Fprintf(&b, "%-39s%s\n", line.what+":", filepath.Join(f.dir, line.name))
	}
	fmt.Fprintf(&b, "%-39s%s\n", "The key this window serves with:", f.hostKey)
	b.WriteString("\nYour SSH keys and known_hosts stay in ~/.ssh, so gridterm and ssh\n" +
		"agree on them. They are not part of what a copy carries.\n\n")

	if f.own {
		fmt.Fprintf(&b,
			"This copy of gridterm carries its own files, in\n\n"+
				"    %s\n\n"+
				"The key this window serves with is in there too, and is only as\n"+
				"private as that directory. Keep the directory somewhere only you\n"+
				"can read.\n", f.beside)
		return b.String()
	}
	fmt.Fprintf(&b,
		"Every copy of gridterm on this machine that does not carry its own\n"+
			"files uses these. To give this copy files of its own:\n\n"+
			"    1. Make the directory\n\n"+
			"           %s\n\n"+
			"    2. Copy the files above into it, so the copy starts with what\n"+
			"       you have now. Leave the key out and this window gets a new\n"+
			"       one, and anything that has taken it over before will refuse\n"+
			"       to connect.\n\n"+
			"    3. Start gridterm again.\n", f.beside)
	return b.String()
}
