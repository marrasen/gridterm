package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/ui"
)

// Everything gridterm remembers goes in the directory conf names, under
// the name an existing user's files are already under. Spelled out
// rather than worked out, so a change of name has to be meant.
func TestWhatGridtermRemembersIsUnderTheNameItAlwaysWas(t *testing.T) {
	home := withHome(t)
	want := filepath.Join(home, "config", "gridterm")

	dir, err := conf.Dir()
	if err != nil {
		t.Fatalf("where its files go: %v", err)
	}
	if dir != want {
		t.Errorf("its files go in %s, want %s", dir, want)
	}

	set, err := settings.Path()
	if err != nil {
		t.Fatalf("the settings: %v", err)
	}
	book, err := remote.BookPath()
	if err != nil {
		t.Fatalf("the saved servers: %v", err)
	}
	for _, at := range []struct{ what, got, want string }{
		{"the settings", set, filepath.Join(want, "settings.json")},
		{"the saved servers", book, filepath.Join(want, "servers.json")},
		{"the colour themes", themes.Path(dir), filepath.Join(want, "themes.json")},
	} {
		if at.got != at.want {
			t.Errorf("%s are at %s, want %s", at.what, at.got, at.want)
		}
	}
}

// aNotice is what a window would show, for a copy sharing the machine's
// files.
func aNotice() string {
	return filesSay(where{
		dir:     `C:\Users\marcus\AppData\Roaming\gridterm`,
		hostKey: `C:\Users\marcus\AppData\Local\gridterm\serve_host_key`,
		beside:  `D:\tools\gridterm-files`,
	})
}

// The notice names every file, each against what it is for.
func TestTheNoticeNamesEveryFileAgainstWhatItIsFor(t *testing.T) {
	got := aNotice()

	for _, line := range []struct{ what, name string }{
		{"Settings", "settings.json"},
		{"Saved servers", "servers.json"},
		{"Themes", "themes.json"},
		{"Shortcuts", "keys.json"},
		{"Authorized keys", "authorized_keys"},
		{"Known windows", "known_windows"},
		{"Serving key", "serve_host_key"},
	} {
		want := line.what + ":"
		at := strings.Index(got, want)
		if at < 0 {
			t.Errorf("the notice does not say %q:\n%s", want, got)
			continue
		}
		end := strings.Index(got[at:], "\n")
		if row := got[at : at+end]; !strings.HasSuffix(row, line.name) {
			t.Errorf("%q is against %q, want it against %s", want, row, line.name)
		}
	}
}

// A copy not carrying its own files is given the button and no steps.
//
// The three steps used to be written out. The button does them, so a
// list of instructions beside it was the dialog describing its own
// button rather than saying anything the button could not.
func TestTheNoticeLeavesTheStepsToTheButton(t *testing.T) {
	got := aNotice()

	for _, step := range []string{"Make the directory", "Copy the files above", "Start gridterm again"} {
		if strings.Contains(got, step) {
			t.Errorf("the notice still spells out %q:\n%s", step, got)
		}
	}
}

// It says the keys under ~/.ssh do not move, which is the one thing the
// list of paths does not itself answer.
func TestTheNoticeSaysWhatIsNotCarried(t *testing.T) {
	got := aNotice()

	if !strings.Contains(got, "~/.ssh") {
		t.Errorf("it does not say the SSH keys stay where they are:\n%s", got)
	}
}

// A copy already carrying its own files says so, and says the key it
// serves with is only as private as the directory it is in.
func TestACopyCarryingItsOwnIsToldAboutItsKey(t *testing.T) {
	got := filesSay(where{
		dir:     `D:\tools\gridterm-files`,
		hostKey: `D:\tools\gridterm-files\serve_host_key`,
		own:     true,
		beside:  `D:\tools\gridterm-files`,
	})

	if !strings.Contains(got, "Portable") {
		t.Errorf("the notice says:\n%s", got)
	}
	if strings.Contains(got, "Make the directory") {
		t.Errorf("it tells the user to make a directory that is there:\n%s", got)
	}
	if !strings.Contains(got, "only as") || !strings.Contains(got, "private") {
		t.Errorf("it does not say the key is only as private as the directory:\n%s", got)
	}
}

// Taking the line shows the notice, laid out as written: the dialog
// would otherwise re-wrap a Windows path at the spaces in it.
func TestTheHelpMenuSaysWhereTheFilesAre(t *testing.T) {
	withHome(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	runFromPalette(t, a, filesCommand)

	n := awaitModal(t, a, "the notice", byTitle[*ui.Notice](filesTitle))
	if !strings.Contains(n.Message(), "settings.json") {
		t.Errorf("it says:\n%s", n.Message())
	}
	if !n.Preformatted {
		t.Error("the notice is re-wrapped, which breaks the paths on it")
	}
}
