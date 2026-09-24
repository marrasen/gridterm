// Package keys reads the file of keyboard shortcuts a user has written.
//
// The file holds changes on top of the shortcuts built in, so shortcuts
// added to a later gridterm still arrive.
package keys

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/newfile"
	"github.com/marrasen/gridterm/ui"
)

// FileVersion is the version this package writes and reads.
const FileVersion = 1

// File is the shortcuts file, in the directory conf names.
const File = "keys.json"

// Nothing is what a chord is bound to when the user wants the shortcut
// built in taken away.
const Nothing = "nothing"

// Renamed maps a command id that has moved to the one it moved to.
//
// A saved shortcut names an id, so renaming one would break a file
// that names it. A line here keeps that file working: it is read when
// the file is applied, and the old name goes on meaning what the user
// meant by it.
//
// Lines stay for good. Taking one out breaks the files this was added
// for, which is the whole point of having it.
var Renamed = map[string]string{}

// ErrDisk marks a failure to read the file itself, as against a file
// read whole and found wrong. A window opens without a file it cannot
// read and cannot be trusted to open with the keys the user asked for.
var ErrDisk = errors.New("the keyboard shortcuts file could not be read")

// Change is one chord and what the file says it should run. Command is
// empty when the chord is to run nothing at all.
type Change struct {
	Chord   ui.Chord
	Command string

	// Written is the chord as the file spells it, for a message about a
	// line the user has to go and find.
	Written string
}

// stored is the shape of the file.
type stored struct {
	Version int `json:"version"`

	// Help is how the file works, written into the file itself.
	//
	// JSON has no comments, so it goes in a field instead. It is read
	// back like any other and then ignored: what matters is that it is
	// in front of whoever opens the file to edit it, which is where the
	// rules are needed and where a dialog cannot reach.
	Help []string `json:"_help,omitempty"`

	// Keys maps a chord to the command it runs, or to "nothing".
	Keys map[string]string `json:"keys"`
}

// startHelp is what a freshly written file says about itself.
var startHelp = []string{
	"This file says what to change, not what the whole window does.",
	"Add a line to put a command on another chord.",
	"To move a command, set its old chord to \"" + Nothing + "\" as well," +
		" or it runs on both.",
	"Delete a line and that chord goes back to what gridterm comes with.",
	"Every chord here runs before a pane sees it, so a chord a program in" +
		" the pane needs stops reaching it.",
	"A dialog that is open sees a chord before either of them.",
}

// Path returns where the file of shortcuts lives, in a directory.
func Path(dir string) string { return filepath.Join(dir, File) }

// Load reads the changes from a file.
//
// A file that is not there returns no changes. Any other failure returns
// none either, so a file read part way cannot change a window part way.
// A failure to read the file at all wraps ErrDisk.
func Load(path string) ([]Change, error) {
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("%w: %s: %w", ErrDisk, path, err)
	}
	var file stored
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON.\n\n%w", path, err)
	}
	if file.Version != FileVersion {
		return nil, fmt.Errorf(
			"%s says version %d, and this gridterm reads version %d",
			path, file.Version, FileVersion)
	}
	// Sorted by what the file spells, so a file with two bad lines names
	// the same one every run.
	written := make([]string, 0, len(file.Keys))
	for chord := range file.Keys {
		written = append(written, chord)
	}
	sort.Strings(written)

	out := make([]Change, 0, len(written))
	seen := make(map[ui.Chord]string, len(written))
	for _, spelling := range written {
		chord, err := ui.ParseChord(spelling)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if had, twice := seen[chord]; twice {
			return nil, fmt.Errorf("%s: %q and %q are the same chord",
				path, had, spelling)
		}
		if err := bindable(chord); err != nil {
			return nil, fmt.Errorf("%s: %q %w", path, spelling, err)
		}
		seen[chord] = spelling
		command := strings.TrimSpace(file.Keys[spelling])
		if command == "" {
			return nil, fmt.Errorf(
				"%s: %q runs nothing at all. Write %q to take the shortcut away",
				path, spelling, Nothing)
		}
		if strings.EqualFold(command, Nothing) {
			command = ""
		}
		out = append(out, Change{Chord: chord, Command: command, Written: spelling})
	}
	return out, nil
}

// bindable says whether a chord can be a window shortcut.
//
// A shortcut runs before the pane sees the key, so one on a chord the
// user types would take that character away everywhere in the window,
// with nothing in gridterm to give it back.
func bindable(c ui.Chord) error {
	if c.Mods&^input.ModShift != 0 {
		return nil
	}
	if !typedInAPane(c.Key) {
		return nil
	}
	return errors.New("is typed in a pane. Hold ctrl, alt or super as well, " +
		"or use a function key")
}

// typedInAPane reports whether a key with no modifier reaches a program
// in a pane as what the user typed.
func typedInAPane(k input.Key) bool {
	if k >= input.KeyA && k <= input.KeyZ {
		return true
	}
	switch k {
	case input.Key0, input.KeyEquals, input.KeyMinus, input.KeySpace,
		input.KeyBracketLeft, input.KeyBracketRight, input.KeyBackslash,
		input.KeyEnter, input.KeyTab, input.KeyBackspace, input.KeyEscape:
		return true
	}
	return false
}

// WriteStart writes a file holding every shortcut a window has now, for
// a user with nowhere to start from.
//
// It refuses a file that is already there: what is in one is the user's.
func WriteStart(path string, have []ui.Binding) error {
	file := stored{
		Version: FileVersion,
		Help:    startHelp,
		Keys:    make(map[string]string, len(have)),
	}
	for _, b := range have {
		file.Keys[b.Chord.String()] = b.ID
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	// Whole or not at all: a starting file cut short by a write that
	// failed would be a file the next attempt refuses to write over.
	err = newfile.Write(path, append(raw, '\n'), 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf(
			"%s is already there. Edit it, or move it aside and take this again", path)
	}
	if err != nil {
		// newfile says which file already.
		return err
	}
	return nil
}
