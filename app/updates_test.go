package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/marrasen/kakel/internal/update"
	"github.com/marrasen/kakel/settings"
)

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{
		"v1.2.3":                 true,
		"v0.1.0":                 true,
		"v1.2.3-4-gabcdef":       false,
		"v1.2.3-4-gabcdef-dirty": false,
		"dev-abcdef":             false,
		"abcdef":                 false,
		"":                       false,
	} {
		if got := isRelease(v); got != want {
			t.Errorf("isRelease(%q) = %v, want %v", v, got, want)
		}
	}
}

// updatesApp is an app with settings of its own.
func updatesApp(t *testing.T) *app {
	a, _ := agentApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	return a
}

// stubRelease answers every look for the newest release with v, and
// counts the looks.
func stubRelease(t *testing.T, v string) *atomic.Int32 {
	was, wasVersion := latestRelease, thisVersion
	var looks atomic.Int32
	latestRelease = func(context.Context) (update.Release, error) {
		looks.Add(1)
		return update.Release{Version: v, Page: "https://example.com/release"}, nil
	}
	thisVersion = func() string { return "v1.0.0" }
	t.Cleanup(func() { latestRelease, thisVersion = was, wasVersion })
	return &looks
}

// With updates off, kakel doesn't even look.
func TestUpdatesOffLooksForNothing(t *testing.T) {
	a := updatesApp(t)
	looks := stubRelease(t, "v99.0.0")
	if err := a.settings.PutUpdates(settings.UpdatesOff); err != nil {
		t.Fatal(err)
	}
	a.lookForUpdate()
	if a.updating || looks.Load() != 0 {
		t.Fatalf("off, it looked %d times", looks.Load())
	}
}

// Told to tell, a newer release is offered; one no newer says nothing.
func TestANewerReleaseIsToldOf(t *testing.T) {
	a := updatesApp(t)
	stubRelease(t, "v1.0.0")
	a.lookForUpdate()
	waitFor(t, a, "the look", func() bool { return !a.updating })
	if len(a.st.Asks) != 0 {
		t.Fatalf("up to date, it asked %+v", a.st.Asks)
	}
	stubRelease(t, "v99.0.0")
	a.lookForUpdate()
	waitFor(t, a, "the offer", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "kakel v99.0.0 is out" || q.Yes != "Update" {
		t.Fatalf("the offer is %+v", q)
	}
}

// Installing copies this program where it is installed, keeps the
// update setting, and offers a restart into the installed copy.
func TestInstallingCopiesAndOffersARestart(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("installs into the user's registry here")
	}
	a := updatesApp(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	exe := filepath.Join(t.TempDir(), "kakel")
	if err := os.WriteFile(exe, []byte("the program"), 0o755); err != nil {
		t.Fatal(err)
	}
	was := executable
	executable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executable = was })
	a.showUpdate()
	if a.st.Update.Installed || !a.st.Update.Installable {
		t.Fatalf("before, the state says %+v", a.st.Update)
	}
	a.handle(InstallKakel{Autostart: true, AutoUpdate: true})
	to := filepath.Join(home, ".local", "bin", "kakel")
	if got, err := os.ReadFile(to); err != nil || string(got) != "the program" {
		t.Fatalf("installed, %s reads %q, %v", to, got, err)
	}
	if a.settings.Updates() != settings.UpdatesInstall || !a.st.Update.Autostart {
		t.Fatalf("installed, updates are %q and the state %+v", a.settings.Updates(), a.st.Update)
	}
	waitFor(t, a, "the restart offer", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "kakel is installed" || q.Yes != "Restart Now" {
		t.Fatalf("the offer is %+v", q)
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID})
	waitFor(t, a, "the offer to close", func() bool { return len(a.st.Asks) == 0 })
	if restartInto != "" {
		t.Fatalf("told later, it restarts into %q", restartInto)
	}
	// The installed copy, started, knows it is the one.
	executable = func() (string, error) { return to, nil }
	a.showUpdate()
	if !a.st.Update.Installed {
		t.Fatal("the installed copy doesn't know it is installed")
	}
}

// A restart the user then declines to quit for isn't done by a quit
// long after.
func TestADeclinedRestartIsForgotten(t *testing.T) {
	a := updatesApp(t)
	t.Cleanup(func() { restartInto = "" })
	a.restartAs("/new/kakel")
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	a.handle(AskAnswered{ID: a.st.Asks[0].ID})
	waitFor(t, a, "the no", func() bool { return !a.leaving })
	if restartInto != "" {
		t.Fatalf("declined, it still restarts into %q", restartInto)
	}
}

// An update put in place isn't fetched again: asked, kakel offers the
// restart into it.
func TestAStagedUpdateIsNotFetchedAgain(t *testing.T) {
	a := updatesApp(t)
	looks := stubRelease(t, "v99.0.0")
	a.staged = "v99.0.0"
	a.lookForUpdate()
	waitFor(t, a, "the look", func() bool { return !a.updating })
	if looks.Load() != 1 || len(a.st.Asks) != 0 {
		t.Fatalf("staged, the look asked %+v", a.st.Asks)
	}
	a.handle(CheckUpdates{})
	waitFor(t, a, "the offer", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "kakel v99.0.0 is ready" || q.Yes != "Restart Now" {
		t.Fatalf("staged, Check for Updates offers %+v", q)
	}
}

// A shell started from the installed program's own folder, as its
// shortcut used to start it, starts at home; any other folder stays.
func TestAShellStartsAtHomeFromTheProgramsFolder(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the installed folder is the user's own here")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := startDir(bin); got != home {
		t.Fatalf("from %s, a shell starts in %s, want %s", bin, got, home)
	}
	work := t.TempDir()
	if got := startDir(work); got != work {
		t.Fatalf("from %s, a shell starts in %s", work, got)
	}
}
