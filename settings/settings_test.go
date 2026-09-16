package settings

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// at is a settings file in a directory the test owns.
func at(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "settings.json")
}

// What is saved comes back on the next run.
func TestWhatIsSavedComesBack(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := s.PutServe(2300, ReachAnywhere); err != nil {
		t.Fatalf("save: %v", err)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	port, have := again.ServePort()
	if !have || port != 2300 {
		t.Errorf("the port came back as %d, %v; want 2300", port, have)
	}
	reach, have := again.ServeReach()
	if !have || reach != ReachAnywhere {
		t.Errorf("the reach came back as %q, %v; want %q", reach, have, ReachAnywhere)
	}
}

// Port 0 is a choice, not an empty one: it means whichever port is free,
// and it has to survive being saved.
func TestPortZeroIsRemembered(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := s.PutServe(0, ReachHere); err != nil {
		t.Fatalf("save: %v", err)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	if port, have := again.ServePort(); !have || port != 0 {
		t.Errorf("the port came back as %d, %v; want 0 saved", port, have)
	}
}

// The file says which version wrote it, so a later shape can be told
// from this one.
func TestTheFileSaysItsVersion(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.PutServe(2300, ReachHere); err != nil {
		t.Fatalf("save: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if !strings.Contains(string(raw), `"version": 1`) {
		t.Errorf("the file has no version in it:\n%s", raw)
	}
}

// A file that is not there is what the first run looks like: nothing is
// remembered and nothing is wrong.
func TestAMissingFileIsNotAnError(t *testing.T) {
	s, err := Load(at(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if port, have := s.ServePort(); have {
		t.Errorf("a missing file remembered port %d", port)
	}
	if reach, have := s.ServeReach(); have {
		t.Errorf("a missing file remembered the reach %q", reach)
	}
	if err := s.Err(); err != nil {
		t.Errorf("a missing file made the settings unsaveable: %v", err)
	}
}

// Settings that could not be read are never written over. The file is
// somebody's, and an empty one in its place loses it for good.
func TestUnreadableSettingsAreNotWrittenOver(t *testing.T) {
	broken := []struct{ what, body string }{
		{"not JSON at all", "{"},
		{"no version", `{"servePort": 2300}`},
		{"a newer version", `{"version": 99, "servePort": 2300}`},
		{"a field this build does not know", `{"version": 1, "fontSize": 14}`},
		{"a port that is not one", `{"version": 1, "servePort": 70000}`},
		{"a reach that means nothing", `{"version": 1, "serveReach": "everywhere"}`},
		{"more than one set of settings", `{"version": 1}{"version": 1}`},
	}
	for _, b := range broken {
		t.Run(b.what, func(t *testing.T) {
			path := at(t)
			if err := os.WriteFile(path, []byte(b.body), 0o600); err != nil {
				t.Fatalf("write the file: %v", err)
			}
			was, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the file: %v", err)
			}

			s, loadErr := Load(path)
			if loadErr == nil {
				t.Fatalf("%s loaded clean", b.what)
			}
			if s.Err() == nil {
				t.Error("the settings do not say they cannot be saved")
			}

			err = s.PutServe(2300, ReachHere)
			if !errors.Is(err, ErrUnsaveable) {
				t.Errorf("saving over it gave %v, want ErrUnsaveable", err)
			}
			now, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the file again: %v", err)
			}
			if !bytes.Equal(was, now) {
				t.Errorf("the file was written over:\n%s\nwant\n%s", now, was)
			}
		})
	}
}

// A file that went bad after it was read is not written over either. The
// file is read again before every change, so a window that has been open
// for a week does not save from a week-old copy.
func TestAFileThatWentBadAfterItWasReadIsNotWrittenOver(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	err = s.PutServe(2300, ReachHere)

	if !errors.Is(err, ErrUnsaveable) {
		t.Errorf("saving gave %v, want ErrUnsaveable", err)
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file again: %v", err)
	}
	if string(now) != "{" {
		t.Errorf("the file was written over:\n%s", now)
	}
}

// Settings with nowhere to live say so and refuse to save.
func TestSettingsWithNowhereToLiveRefuseToSave(t *testing.T) {
	s := Unusable(errors.New("no configuration directory"))

	if s.Err() == nil {
		t.Error("they do not say why they are unusable")
	}
	if err := s.PutServe(2300, ReachHere); !errors.Is(err, ErrUnsaveable) {
		t.Errorf("saving gave %v, want ErrUnsaveable", err)
	}
}

// A write that fails leaves the settings that were there, and leaves no
// half-written file behind.
func TestAFailedWriteLeavesTheOldFile(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.PutServe(2300, ReachHere); err != nil {
		t.Fatalf("first save: %v", err)
	}
	was, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file: %v", err)
	}

	// The disk gives up on the last step, after the new file is written
	// and before it takes the old one's place.
	wasRename := rename
	rename = func(string, string) error { return errors.New("the disk gave up") }
	t.Cleanup(func() { rename = wasRename })

	err = s.PutServe(9000, ReachAnywhere)

	if err == nil {
		t.Fatal("a failed write was reported as a save")
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file again: %v", err)
	}
	if !bytes.Equal(was, now) {
		t.Errorf("the old settings are gone:\n%s\nwant\n%s", now, was)
	}
	left, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read the directory: %v", err)
	}
	if len(left) != 1 {
		names := make([]string, 0, len(left))
		for _, e := range left {
			names = append(names, e.Name())
		}
		t.Errorf("the directory holds %v, want only the settings", names)
	}
	// And the settings still hold what is on disk, not what failed to
	// get there.
	if port, _ := s.ServePort(); port != 2300 {
		t.Errorf("the settings hold port %d, want the 2300 that is saved", port)
	}
}
