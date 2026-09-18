package conf

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// aCopyAt is the path a copy of gridterm would sit at, in a directory
// the test owns.
func aCopyAt(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "gridterm.exe")
}

// withConfigHome points os.UserConfigDir at a directory the test owns,
// and returns it. The real one belongs to whoever runs the tests.
func withConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("APPDATA", home)
	return home
}

// asACopyAt makes Dir, Private and CarriesItsOwn answer for a copy of
// gridterm at exe, and forgets the answer again afterwards.
func asACopyAt(t *testing.T, exe string) {
	t.Helper()
	forget := func() {
		made, once = choice{}, sync.Once{}
	}
	forget()
	findExe = func() (string, error) { return exe, nil }
	t.Cleanup(func() {
		findExe = whereGridtermIs
		forget()
	})
}

// The name gridterm's directory is under does not change: an existing
// user's settings, servers and colour themes are already under it.
func TestTheDirectoryKeepsTheNameItHasAlwaysHad(t *testing.T) {
	if Name != "gridterm" {
		t.Errorf("the directory is called %q, and everybody's files are under \"gridterm\"", Name)
	}
}

// The one beside the executable is called something else, so a system
// where the executable is called "gridterm" cannot have the name twice.
func TestTheDirectoryBesideACopyCannotBeTheCopy(t *testing.T) {
	exe := filepath.Join(`C:\tools`, "gridterm")

	if got := Beside(exe); got == exe {
		t.Errorf("the directory beside it is %s, which is the executable", got)
	}
}

// A copy with nothing beside it keeps its files where the operating
// system puts a program's.
func TestACopyWithNothingBesideItUsesTheSystemsDirectory(t *testing.T) {
	home := withConfigHome(t)
	exe := aCopyAt(t)

	got, err := DirFor(exe)

	if err != nil {
		t.Fatalf("where its files go: %v", err)
	}
	if want := filepath.Join(home, "gridterm"); got != want {
		t.Errorf("its files go in %s, want %s", got, want)
	}
}

// A copy with a directory of its own beside it uses that, so one machine
// can hold several copies with files of their own.
func TestACopyWithADirectoryBesideItCarriesItsOwn(t *testing.T) {
	withConfigHome(t)
	exe := aCopyAt(t)
	beside := aDirectoryBeside(t, exe)

	got, err := DirFor(exe)

	if err != nil {
		t.Fatalf("where its files go: %v", err)
	}
	if got != beside {
		t.Errorf("its files go in %s, want the %s beside it", got, beside)
	}

	own, where, err := CarriesItsOwnFor(exe)
	if err != nil {
		t.Fatalf("does it carry its own: %v", err)
	}
	if !own || where != beside {
		t.Errorf("it says %v, %s", own, where)
	}
}

// A file of that name beside a copy is not a directory of its own: the
// files would have nowhere to go.
func TestAFileOfThatNameBesideACopyIsNotADirectory(t *testing.T) {
	home := withConfigHome(t)
	exe := aCopyAt(t)
	if err := os.WriteFile(Beside(exe), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}

	got, err := DirFor(exe)

	if err != nil {
		t.Fatalf("where its files go: %v", err)
	}
	if want := filepath.Join(home, "gridterm"); got != want {
		t.Errorf("its files go in %s, want %s", got, want)
	}
}

// A path that cannot be looked at stops the answer. Saying "there is no
// directory beside it" would send a user's files somewhere else without
// a word, and the private ones somewhere less private.
func TestAPathThatCannotBeLookedAtStopsTheAnswer(t *testing.T) {
	// A name no filesystem takes, so the look fails for a reason that is
	// not "it is not there".
	exe := filepath.Join("bad\x00name", "gridterm.exe")

	for _, c := range []struct {
		what string
		ask  func(string) (string, error)
	}{
		{"where its files go", DirFor},
		{"where its private files go", PrivateFor},
	} {
		withConfigHome(t)

		_, err := c.ask(exe)

		if err == nil {
			t.Errorf("%s: a path that could not be looked at was answered for", c.what)
			continue
		}
		if !strings.Contains(err.Error(), "look at") {
			t.Errorf("%s: it says %q", c.what, err)
		}
	}
}

// A copy carrying its own files keeps its private ones there too: the
// point of carrying them is that the copy holds everything.
func TestACopyCarryingItsOwnKeepsItsPrivateFilesThere(t *testing.T) {
	withConfigHome(t)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	exe := aCopyAt(t)
	beside := aDirectoryBeside(t, exe)

	got, err := PrivateFor(exe)

	if err != nil {
		t.Fatalf("where its private files go: %v", err)
	}
	if got != beside {
		t.Errorf("they go in %s, want the %s beside it", got, beside)
	}
}

// A copy sharing the machine's directory keeps its private files off the
// roaming profile, which a domain account copies to a file server.
func TestPrivateFilesStayOffTheRoamingProfile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows has a roaming profile to stay off")
	}
	withConfigHome(t)
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	exe := aCopyAt(t)

	got, err := PrivateFor(exe)

	if err != nil {
		t.Fatalf("where its private files go: %v", err)
	}
	if want := filepath.Join(local, "gridterm"); got != want {
		t.Errorf("they go in %s, want %s", got, want)
	}
}

// Dir, Private and CarriesItsOwn answer for the copy of gridterm that is
// running.
func TestGridtermAsksAboutItself(t *testing.T) {
	withConfigHome(t)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	exe := aCopyAt(t)
	beside := aDirectoryBeside(t, exe)
	asACopyAt(t, exe)

	dir, err := Dir()
	if err != nil {
		t.Fatalf("where its files go: %v", err)
	}
	private, err := Private()
	if err != nil {
		t.Fatalf("where its private files go: %v", err)
	}
	own, where, err := CarriesItsOwn()
	if err != nil {
		t.Fatalf("does it carry its own: %v", err)
	}

	if dir != beside || private != beside {
		t.Errorf("its files go in %s and %s, want both in %s", dir, private, beside)
	}
	if !own || where != beside {
		t.Errorf("it says %v, %s", own, where)
	}
}

// The answer is worked out once. A directory made while gridterm is
// running would otherwise send the rest of the session's writes
// somewhere else, leaving a window half in each place.
func TestADirectoryMadeWhileGridtermIsRunningChangesNothing(t *testing.T) {
	home := withConfigHome(t)
	t.Setenv("LOCALAPPDATA", home)
	exe := aCopyAt(t)
	asACopyAt(t, exe)
	was, err := Dir()
	if err != nil {
		t.Fatalf("where its files go: %v", err)
	}

	aDirectoryBeside(t, exe)

	got, err := Dir()
	if err != nil {
		t.Fatalf("where its files go now: %v", err)
	}
	if got != was {
		t.Errorf("its files moved to %s part way through, from %s", got, was)
	}
	own, _, err := CarriesItsOwn()
	if err != nil {
		t.Fatalf("does it carry its own: %v", err)
	}
	if own {
		t.Error("it says it carries its own files, having been told otherwise already")
	}
}

// A copy whose own path cannot be worked out says so rather than
// guessing at one of the two places.
func TestACopyThatCannotFindItselfSaysSo(t *testing.T) {
	withConfigHome(t)
	asACopyAt(t, "")
	findExe = func() (string, error) { return "", errors.New("no such process") }

	_, err := Dir()

	if err == nil {
		t.Fatal("a copy that could not find itself answered anyway")
	}
	if !strings.Contains(err.Error(), "no such process") {
		t.Errorf("it says %q", err)
	}
}

// aDirectoryBeside makes the directory a copy of gridterm at exe carries
// its files in, and returns it.
func aDirectoryBeside(t *testing.T, exe string) string {
	t.Helper()
	beside := Beside(exe)
	if err := os.Mkdir(beside, 0o700); err != nil {
		t.Fatalf("make it: %v", err)
	}
	return beside
}
