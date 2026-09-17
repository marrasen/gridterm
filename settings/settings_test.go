package settings

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
		{"an agent host with no name", `{"version": 1, "agentHost": ""}`},
		{"a shell with no id", `{"version": 1, "shell": ""}`},
		{"more than one set of settings", `{"version": 1}{"version": 1}`},
		{"a key written twice", `{"version": 1, "servePort": 2300, "servePort": 9000}`},
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

// The one key written twice is named, so the user knows what to repair.
//
// Go's decoder keeps the last of a repeated key, so a file asking for
// two ports would quietly become whichever came second -- and the next
// save would make that permanent.
func TestAKeyWrittenTwiceIsNamed(t *testing.T) {
	path := at(t)
	const twice = `{"version": 1, "servePort": 2300, "servePort": 9000}`
	if err := os.WriteFile(path, []byte(twice), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	_, err := Load(path)

	if err == nil {
		t.Fatal("a file asking for two ports loaded as one")
	}
	if !strings.Contains(err.Error(), "servePort") || !strings.Contains(err.Error(), "twice") {
		t.Errorf("it said %v, without naming what is in it twice", err)
	}
}

// Settings from a newer gridterm say so, even when that gridterm also
// added a field this build does not know.
//
// The strict decode used to run first, so the user was told "json:
// unknown field" about a file whose real trouble is that it belongs to a
// later version.
func TestNewerSettingsSaySoRatherThanNamingTheirNewField(t *testing.T) {
	path := at(t)
	const newer = `{"version": 2, "servePort": 2300, "fontSize": 14}`
	if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	_, err := Load(path)

	if err == nil {
		t.Fatal("settings from a newer gridterm loaded clean")
	}
	if !strings.Contains(err.Error(), "newer gridterm") {
		t.Errorf("it said %v, without saying the file is from a newer gridterm", err)
	}
}

// A settings file linked in from somewhere else goes on being the file
// that is written, rather than being quietly replaced by a copy.
func TestSettingsSaveThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if err := os.WriteFile(real, []byte(`{"version": 1}`), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	s, err := Load(link)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := s.PutServe(2300, ReachHere); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced by a file, so the real one stopped being updated")
	}
	raw, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("read the real file: %v", err)
	}
	if !strings.Contains(string(raw), "2300") {
		t.Errorf("the real file was not updated:\n%s", raw)
	}
}

// Two goroutines saving at once both get an answer, and what is on disk
// is one of the two rather than a mix.
func TestSavingFromTwoGoroutines(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	done := make(chan error, 2)
	for _, port := range []int{2300, 9000} {
		go func() { done <- s.PutServe(port, ReachHere) }()
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	port, saved := again.ServePort()
	if !saved || (port != 2300 && port != 9000) {
		t.Errorf("the file holds port %d, %v; want one of the two that were saved", port, saved)
	}
}

// Which agent the hand-over dialog was set to comes back on the next
// run.
func TestTheAgentHostIsRemembered(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if name, have := s.AgentHost(); have {
		t.Errorf("a missing file remembered the agent %q", name)
	}

	if err := s.PutAgentHost("Codex"); err != nil {
		t.Fatalf("save: %v", err)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	name, have := again.AgentHost()
	if !have || name != "Codex" {
		t.Errorf("the agent came back as %q, %v; want Codex", name, have)
	}
}

// Remembering the agent leaves what else was saved alone, because it
// reads the file before it writes it.
func TestRememberingTheAgentKeepsWhatElseWasSaved(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.PutServe(2300, ReachAnywhere); err != nil {
		t.Fatalf("save the port: %v", err)
	}

	if err := s.PutAgentHost("Cursor"); err != nil {
		t.Fatalf("save the agent: %v", err)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	if port, have := again.ServePort(); !have || port != 2300 {
		t.Errorf("the port came back as %d, %v; want 2300", port, have)
	}
	if name, have := again.AgentHost(); !have || name != "Cursor" {
		t.Errorf("the agent came back as %q, %v; want Cursor", name, have)
	}
}

// Settings that could not be read are not written over by remembering an
// agent either.
func TestTheAgentIsNotSavedOverUnreadableSettings(t *testing.T) {
	path := at(t)
	const broken = "{"
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	s, loadErr := Load(path)
	if loadErr == nil {
		t.Fatal("a broken file loaded clean")
	}

	err := s.PutAgentHost("Codex")

	if !errors.Is(err, ErrUnsaveable) {
		t.Errorf("saving gave %v, want ErrUnsaveable", err)
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file again: %v", err)
	}
	if string(now) != broken {
		t.Errorf("the file was written over:\n%s", now)
	}
}

// An agent with no name is not a choice, and is refused rather than
// written down.
func TestAnAgentWithNoNameIsRefused(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := s.PutAgentHost(""); err == nil {
		t.Error("an agent with no name was saved")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		raw, _ := os.ReadFile(path)
		t.Errorf("a file was written anyway:\n%s", raw)
	}
}

// Which shell a new pane runs comes back on the next run.
func TestTheShellIsRemembered(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if id, have := s.Shell(); have {
		t.Errorf("a missing file remembered the shell %q", id)
	}

	if err := s.PutShell("wsl:Ubuntu"); err != nil {
		t.Fatalf("save: %v", err)
	}

	if id, have := s.Shell(); !have || id != "wsl:Ubuntu" {
		t.Errorf("the shell reads back as %q, %v; want wsl:Ubuntu", id, have)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	id, have := again.Shell()
	if !have || id != "wsl:Ubuntu" {
		t.Errorf("the shell came back as %q, %v; want wsl:Ubuntu", id, have)
	}
}

// Remembering the shell leaves what else the same settings saved alone.
// Another window's work is TestRememberingTheShellKeepsASecondWindowsWork.
func TestRememberingTheShellKeepsWhatElseWasSaved(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.PutServe(2300, ReachAnywhere); err != nil {
		t.Fatalf("save the port: %v", err)
	}
	if err := s.PutAgentHost("Codex"); err != nil {
		t.Fatalf("save the agent: %v", err)
	}

	if err := s.PutShell("pwsh"); err != nil {
		t.Fatalf("save the shell: %v", err)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	if port, have := again.ServePort(); !have || port != 2300 {
		t.Errorf("the port came back as %d, %v; want 2300", port, have)
	}
	if reach, have := again.ServeReach(); !have || reach != ReachAnywhere {
		t.Errorf("the reach came back as %q, %v; want %q", reach, have, ReachAnywhere)
	}
	if name, have := again.AgentHost(); !have || name != "Codex" {
		t.Errorf("the agent came back as %q, %v; want Codex", name, have)
	}
	if id, have := again.Shell(); !have || id != "pwsh" {
		t.Errorf("the shell came back as %q, %v; want pwsh", id, have)
	}
}

// Remembering the shell does not write over a second window's work, because
// it reads the file again first.
func TestRememberingTheShellKeepsASecondWindowsWork(t *testing.T) {
	path := at(t)
	one, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	two, err := Load(path)
	if err != nil {
		t.Fatalf("load a second window: %v", err)
	}
	if err := two.PutServe(9000, ReachAnywhere); err != nil {
		t.Fatalf("the second window saves the port: %v", err)
	}

	if err := one.PutShell("cmd"); err != nil {
		t.Fatalf("save the shell: %v", err)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	if port, have := again.ServePort(); !have || port != 9000 {
		t.Errorf("the port came back as %d, %v; want the 9000 the second window saved", port, have)
	}
	if id, have := again.Shell(); !have || id != "cmd" {
		t.Errorf("the shell came back as %q, %v; want cmd", id, have)
	}
}

// A shell with no id is not a choice, and is refused rather than written
// down.
func TestAShellWithNoIdIsRefused(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := s.PutShell(""); err == nil {
		t.Error("a shell with no id was saved")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		raw, _ := os.ReadFile(path)
		t.Errorf("a file was written anyway:\n%s", raw)
	}
}

// A file naming a shell with no id is turned away in words the user can
// act on.
func TestAFileWithAnEmptyShellSaysWhatIsWrong(t *testing.T) {
	path := at(t)
	if err := os.WriteFile(path, []byte(`{"version": 1, "shell": ""}`), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}

	_, err := Load(path)

	if err == nil {
		t.Fatal("a shell with no id loaded clean")
	}
	if !strings.Contains(err.Error(), "shell") {
		t.Errorf("it said %v, without saying the shell is what is wrong", err)
	}
}

// Settings that could not be read are not written over by remembering a
// shell either.
func TestTheShellIsNotSavedOverUnreadableSettings(t *testing.T) {
	path := at(t)
	const broken = "{"
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	s, loadErr := Load(path)
	if loadErr == nil {
		t.Fatal("a broken file loaded clean")
	}

	err := s.PutShell("pwsh")

	if !errors.Is(err, ErrUnsaveable) {
		t.Errorf("saving gave %v, want ErrUnsaveable", err)
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the file again: %v", err)
	}
	if string(now) != broken {
		t.Errorf("the file was written over:\n%s", now)
	}
}

// A write that fails leaves the shell that was saved, on disk and in the
// settings.
func TestAFailedWriteLeavesTheShellThatWasSaved(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := s.PutShell("cmd"); err != nil {
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

	err = s.PutShell("pwsh")

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
	if id, _ := s.Shell(); id != "cmd" {
		t.Errorf("the settings hold the shell %q, want the cmd that is saved", id)
	}
}

// Two goroutines remembering different things both survive, which is
// what the reread before every save is for.
func TestSavingDifferentThingsAtOnce(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	done := make(chan error, 2)
	go func() { done <- s.PutShell("pwsh") }()
	go func() { done <- s.PutAgentHost("Codex") }()
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("load again: %v", err)
	}
	if id, have := again.Shell(); !have || id != "pwsh" {
		t.Errorf("the shell came back as %q, %v; want pwsh", id, have)
	}
	if name, have := again.AgentHost(); !have || name != "Codex" {
		t.Errorf("the agent came back as %q, %v; want Codex", name, have)
	}
}

// Settings that cannot be read refuse to remember a shell.
func TestTheShellIsNotSavedOverUnusableSettings(t *testing.T) {
	s := Unusable(errors.New("no configuration directory"))

	if err := s.PutShell("cmd"); !errors.Is(err, ErrUnsaveable) {
		t.Errorf("saving gave %v, want ErrUnsaveable", err)
	}
}

// A saved command with nothing to run names nothing, so the file is
// turned away rather than read back as a command that cannot be offered.
func TestASavedCommandWithNothingToRunIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	raw := `{"version":1,"commands":[{"line":"make deploy"},{"line":""}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("a command with nothing to run was read back")
	}
	// The one that is wrong, by the place it sits in the file: a message
	// that does not say which cannot be acted on.
	if !strings.Contains(err.Error(), "saved command 2") {
		t.Errorf("it said %q, want it to name the second command", err)
	}
}

// The same line twice is turned away: the dialog steps through the
// commands, and stepping from one to its twin lands on itself.
func TestASavedCommandTwiceOverIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	raw := `{"version":1,"commands":[{"line":"make deploy"},{"line":"make deploy"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("the same command twice was read back")
	}
	if !strings.Contains(err.Error(), "twice") {
		t.Errorf("it said %q", err)
	}
}

// And a file whose commands are all good is read back whole.
func TestSavedCommandsAreReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	raw := `{"version":1,"commands":[` +
		`{"line":"make deploy","dir":"/src","host":"margit"},{"line":"ls -la"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	set, err := Load(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := set.Commands()
	want := []SavedCommand{
		{Line: "make deploy", Dir: "/src", Host: "margit"},
		{Line: "ls -la"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("it read %v, want %v", got, want)
	}
}

// What Commands gives back is the caller's own, so changing it cannot
// change what is saved.
func TestCommandsGivesBackACopy(t *testing.T) {
	set := settingsAt(t)
	if err := set.KeepCommand(SavedCommand{Line: "make deploy"}, 10); err != nil {
		t.Fatalf("keep: %v", err)
	}

	got := set.Commands()
	got[0].Line = "rm -rf /"

	if again := set.Commands(); again[0].Line != "make deploy" {
		t.Errorf("the settings now say %q", again[0].Line)
	}
}

// Keeping a command puts it at the front, and keeping one already there
// moves it rather than doubling it.
func TestKeepCommandPutsItAtTheFront(t *testing.T) {
	set := settingsAt(t)
	for _, line := range []string{"first", "second", "third"} {
		if err := set.KeepCommand(SavedCommand{Line: line, Dir: "/old"}, 10); err != nil {
			t.Fatalf("keep %q: %v", line, err)
		}
	}

	if err := set.KeepCommand(SavedCommand{Line: "first", Dir: "/new"}, 10); err != nil {
		t.Fatalf("keep it again: %v", err)
	}

	got := set.Commands()
	want := []SavedCommand{
		{Line: "first", Dir: "/new"},
		{Line: "third", Dir: "/old"},
		{Line: "second", Dir: "/old"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("it holds %v, want %v", got, want)
	}
}

// The list is capped, and it is the one kept longest ago that goes.
func TestKeepCommandDropsTheOldest(t *testing.T) {
	set := settingsAt(t)
	for i := 0; i < 6; i++ {
		if err := set.KeepCommand(SavedCommand{Line: "echo " + strconv.Itoa(i)}, 4); err != nil {
			t.Fatalf("keep %d: %v", i, err)
		}
	}

	var got []string
	for _, cmd := range set.Commands() {
		got = append(got, cmd.Line)
	}
	want := []string{"echo 5", "echo 4", "echo 3", "echo 2"}
	if !slices.Equal(got, want) {
		t.Errorf("it holds %v, want %v", got, want)
	}
}

// DropCommand takes one out and leaves the rest in order.
func TestDropCommandTakesOneOut(t *testing.T) {
	set := settingsAt(t)
	for _, line := range []string{"first", "second", "third"} {
		if err := set.KeepCommand(SavedCommand{Line: line}, 10); err != nil {
			t.Fatalf("keep %q: %v", line, err)
		}
	}

	if err := set.DropCommand("second"); err != nil {
		t.Fatalf("drop: %v", err)
	}

	var got []string
	for _, cmd := range set.Commands() {
		got = append(got, cmd.Line)
	}
	if want := []string{"third", "first"}; !slices.Equal(got, want) {
		t.Errorf("it holds %v, want %v", got, want)
	}
}

// A command another window saved while this one was not looking is
// still there afterwards. The list is edited after the file is reread,
// not before.
func TestKeepCommandDoesNotLoseWhatAnotherWindowSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	one, err := Load(path)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	two, err := Load(path)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	// Read by both before either writes, which is what makes one of
	// them stale.
	_, _ = one.Commands(), two.Commands()

	if err := one.KeepCommand(SavedCommand{Line: "make deploy"}, 10); err != nil {
		t.Fatalf("the first window: %v", err)
	}
	if err := two.KeepCommand(SavedCommand{Line: "ls -la"}, 10); err != nil {
		t.Fatalf("the second window: %v", err)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("read it again: %v", err)
	}
	var got []string
	for _, cmd := range again.Commands() {
		got = append(got, cmd.Line)
	}
	if want := []string{"ls -la", "make deploy"}; !slices.Equal(got, want) {
		t.Errorf("the file holds %v, want both windows' commands", got)
	}
}

// settingsAt is settings in a directory the test owns.
func settingsAt(t *testing.T) *Settings {
	t.Helper()
	set, err := Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	return set
}
