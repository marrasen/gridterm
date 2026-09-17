package keys

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// write puts a shortcuts file in a directory.
func write(t *testing.T, dir, body string) string {
	t.Helper()
	at := Path(dir)
	if err := os.WriteFile(at, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return at
}

// A file that is not there is what a window with no shortcuts of its own
// looks like: no changes and no complaint.
func TestNoFileChangesNothing(t *testing.T) {
	got, err := Load(Path(t.TempDir()))

	if err != nil {
		t.Fatalf("no file: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("it changes %v", got)
	}
}

// A line moves a chord to a command.
func TestALineMovesAChordToACommand(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"keys":{"ctrl+shift+P":"palette.open"}}`)

	got, err := Load(Path(dir))

	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("it changes %v", got)
	}
	want := ui.Chord{Key: input.KeyP, Mods: input.ModCtrl | input.ModShift}
	if got[0].Chord != want {
		t.Errorf("it changes %s, want %s", got[0].Chord, want)
	}
	if got[0].Command != "palette.open" {
		t.Errorf("it runs %q", got[0].Command)
	}
	if got[0].Written != "ctrl+shift+P" {
		t.Errorf("it remembers the line as %q, want it as the file spells it", got[0].Written)
	}
}

// A chord set to "nothing" takes the built-in shortcut away.
func TestAChordSetToNothingTakesTheShortcutAway(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"keys":{"F10":"Nothing"}}`)

	got, err := Load(Path(dir))

	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("it changes %v", got)
	}
	if got[0].Command != "" {
		t.Errorf("F10 runs %q, want nothing", got[0].Command)
	}
}

// A chord nobody can read is turned away when the file is read, rather
// than leaving a window whose keys half work.
func TestAChordNobodyCanReadIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"keys":{"ctrl+shift+P":"palette.open","hyper+Q":"pane.close"}}`)

	got, err := Load(Path(dir))

	if err == nil {
		t.Fatal("a chord nobody can read was accepted")
	}
	if len(got) != 0 {
		t.Errorf("it changes %v, want none of a file it could not read", got)
	}
	if !strings.Contains(err.Error(), "hyper") {
		t.Errorf("it says %q, want it to name the line", err)
	}
}

// Two lines spelling the same chord are turned away: one would win and
// the user could not tell which.
func TestTwoLinesForTheSameChordAreRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"keys":{"ctrl+shift+P":"palette.open","shift+ctrl+p":"pane.close"}}`)

	_, err := Load(Path(dir))

	if err == nil {
		t.Fatal("a chord written twice was accepted")
	}
	if !strings.Contains(err.Error(), "same chord") {
		t.Errorf("it says %q", err)
	}
}

// A file from a version this gridterm does not read is said so rather
// than guessed at.
func TestAFileFromAnotherVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":99,"keys":{}}`)

	_, err := Load(Path(dir))

	if err == nil {
		t.Fatal("a file from another version was read")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("it says %q", err)
	}
}

// A file that is not JSON at all says so rather than leaving a window
// with the shortcuts the user thought they had changed.
func TestAFileThatIsNotJSONIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ctrl+shift+P = palette.open")

	got, err := Load(Path(dir))

	if err == nil {
		t.Fatal("a file that is not JSON was read")
	}
	if len(got) != 0 {
		t.Errorf("it changes %v", got)
	}
}

// The lines come back sorted by what the file spells, so a file with two
// bad lines names the same one each time.
func TestTheLinesComeBackSortedByWhatTheFileSpells(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"keys":{"ctrl+shift+Z":"pane.close",`+
		`"ctrl+shift+A":"pane.open","F9":"palette.open"}}`)

	got, err := Load(Path(dir))

	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var order []string
	for _, c := range got {
		order = append(order, c.Written)
	}
	want := []string{"F9", "ctrl+shift+A", "ctrl+shift+Z"}
	if !slices.Equal(order, want) {
		t.Errorf("it gave %v, want %v", order, want)
	}
}

// A chord bound to nothing at all is a mistake: a user who blanks a
// value would otherwise lose the shortcut with no word about it.
func TestAChordBoundToNothingAtAllIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"keys":{"ctrl+shift+P":"  "}}`)

	got, err := Load(Path(dir))

	if err == nil {
		t.Fatal("a chord bound to nothing at all was accepted")
	}
	if len(got) != 0 {
		t.Errorf("it changes %v", got)
	}
	if !strings.Contains(err.Error(), Nothing) {
		t.Errorf("it says %q, want it to say how to take a shortcut away", err)
	}
}

// A file the disk cannot read is told apart from one read whole and
// found wrong, because a window opens without the second and not the
// first.
func TestAFileTheDiskCannotReadIsMarkedAsSuch(t *testing.T) {
	dir := t.TempDir()
	// A directory where the file goes, so the read fails for a reason
	// that is not "it is not there".
	if err := os.Mkdir(Path(dir), 0o700); err != nil {
		t.Fatalf("make it: %v", err)
	}

	_, err := Load(Path(dir))

	if !errors.Is(err, ErrDisk) {
		t.Errorf("it says %q, want a disk failure", err)
	}
}

// And a file read whole and found wrong is not marked as one.
func TestAFileFoundWrongIsNotADiskFailure(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "not JSON at all")

	_, err := Load(Path(dir))

	if err == nil {
		t.Fatal("a file that is not JSON was read")
	}
	if errors.Is(err, ErrDisk) {
		t.Errorf("it says %q, want it told apart from a disk failure", err)
	}
}

// A starting file that cannot be written says so rather than leaving the
// user thinking there is one.
func TestAStartingFileThatCannotBeWrittenSaysSo(t *testing.T) {
	dir := t.TempDir()
	// A file where the directory goes, so making it fails.
	blocked := filepath.Join(dir, "in the way")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}

	err := WriteStart(Path(blocked), nil)

	if err == nil {
		t.Fatal("a file that could not be written was reported as written")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("it says %q", err)
	}
}

// A starting file holds every shortcut a window has, and reads back.
func TestAStartingFileReadsBack(t *testing.T) {
	dir := t.TempDir()
	have := []ui.Binding{
		{Chord: ui.Chord{Key: input.KeyK, Mods: input.ModCtrl | input.ModShift}, ID: "palette.open"},
		{Chord: ui.Chord{Key: input.KeyF10}, ID: "menu.open"},
	}

	if err := WriteStart(Path(dir), have); err != nil {
		t.Fatalf("write it: %v", err)
	}

	got, err := Load(Path(dir))
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if len(got) != len(have) {
		t.Fatalf("it holds %v", got)
	}
	ran := make(map[ui.Chord]string, len(got))
	for _, c := range got {
		ran[c.Chord] = c.Command
	}
	for _, b := range have {
		if ran[b.Chord] != b.ID {
			t.Errorf("%s runs %q, want %q", b.Chord, ran[b.Chord], b.ID)
		}
	}
}

// And a file that is already there is not written over: what is in one
// is the user's.
func TestAStartingFileDoesNotWriteOverOne(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, `{"version":1,"keys":{}}`)

	err := WriteStart(Path(dir), nil)

	if err == nil {
		t.Fatal("it wrote over a file that was there")
	}
	if !strings.Contains(err.Error(), "already there") {
		t.Errorf("it says %q", err)
	}
}

// The file lives beside the settings.
func TestTheFileLivesBesideTheSettings(t *testing.T) {
	if got, want := Path(`C:\x`), filepath.Join(`C:\x`, "keys.json"); got != want {
		t.Errorf("it is at %s, want %s", got, want)
	}
}

// A chord the user would type is refused: a shortcut runs before the
// pane sees the key, so one on a plain letter would take that letter
// away everywhere, with nothing in gridterm to give it back.
func TestAChordTheUserWouldTypeIsRefused(t *testing.T) {
	for _, spelling := range []string{"k", "shift+K", "Enter", "shift+Tab", "Escape", "Space", "0"} {
		dir := t.TempDir()
		write(t, dir, `{"version":1,"keys":{"`+spelling+`":"pane.close"}}`)

		got, err := Load(Path(dir))

		if err == nil {
			t.Errorf("%q was taken as a shortcut", spelling)
			continue
		}
		if len(got) != 0 {
			t.Errorf("%q changes %v", spelling, got)
		}
		if !strings.Contains(err.Error(), "ctrl") {
			t.Errorf("%q says %q, want it to say what to hold as well", spelling, err)
		}
	}
}

// A chord the pane does not type is taken, so the shortcuts gridterm
// comes with can all be written in the file.
func TestAChordThePaneDoesNotTypeIsTaken(t *testing.T) {
	for _, spelling := range []string{
		"ctrl+shift+K", "alt+K", "super+K", "F10", "shift+Insert", "ctrl+Tab", "shift+PageUp",
	} {
		dir := t.TempDir()
		write(t, dir, `{"version":1,"keys":{"`+spelling+`":"pane.close"}}`)

		got, err := Load(Path(dir))

		if err != nil {
			t.Errorf("%q: %v", spelling, err)
			continue
		}
		if len(got) != 1 {
			t.Errorf("%q changes %v", spelling, got)
		}
	}
}
