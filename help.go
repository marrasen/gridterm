package main

import (
	"cmp"
	"slices"
	"strings"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
)

// helpCommand is the command that lists the keys, and helpTitle names
// both the dialog and the line that opens it.
const (
	helpCommand = "help.keys"
	helpTitle   = "Keys and commands"
)

// helpGap is the blank between a line's words and its chord.
const helpGap = 2

// showHelp lists every command and the key that runs it.
func (a *app) showHelp() error {
	n := a.newNotice(helpTitle, a.helpText())
	// Laid out in columns, which the dialog would otherwise re-wrap at
	// spaces and lose.
	n.Preformatted = true
	a.presentNotice(n)
	return nil
}

// helpText lists the commands under the menu each hangs on, then the
// keys no menu shows, then the file browser's own keys, read from the
// registry and the keymaps so it cannot drift from what the keys do.
//
// Left out: the commands the window builds from the server list and the
// font scan, because there is one per machine and one per family and
// the menus that generate them already list them.
func (a *app) helpText() string {
	sections := a.commandSections()
	sections = append(sections, helpSection{
		title: "The file browser's keys, which the shortcuts file does not change",
		lines: browserHelp(),
	})
	sections = append(sections, helpSection{
		title: "The file viewer's keys, which the shortcuts file does not change",
		lines: readerHelp(),
	})
	sections = append(sections, helpSection{
		title: "What the keyboard shortcuts file calls each command",
		lines: a.commandNames(),
		tight: true,
	})

	// One column across the whole list, so the chords line up from the
	// top of the dialog to the bottom. A tight section keeps its own.
	width := 0
	for _, s := range sections {
		if s.tight {
			continue
		}
		for _, l := range s.lines {
			width = max(width, grid.StringWidth(l.what))
		}
	}

	var out []string
	for _, s := range sections {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, s.title)
		at := width
		if s.tight {
			at = 0
			for _, l := range s.lines {
				at = max(at, grid.StringWidth(l.what))
			}
		}
		for _, l := range s.lines {
			out = append(out, "  "+l.write(at))
		}
	}
	return strings.Join(out, "\n")
}

// helpLine is one thing the user can do and the chord that does it.
// The chord is empty when none is bound.
type helpLine struct{ what, chord string }

// write pads the line out so every chord starts in the same column.
func (l helpLine) write(width int) string {
	if l.chord == "" {
		return l.what
	}
	pad := width - grid.StringWidth(l.what) + helpGap
	return l.what + strings.Repeat(" ", max(pad, 1)) + l.chord
}

// helpSection is one heading of the list and the lines under it. A
// tight section lines its own second column up rather than the one the
// rest of the list shares.
type helpSection struct {
	title string
	lines []helpLine
	tight bool
}

// commandNames lists what the shortcuts file calls each command, against
// the title the menus and the palette show.
func (a *app) commandNames() []helpLine {
	var out []helpLine
	for _, cmd := range a.root.Commands.All() {
		if generatedCommand(cmd.ID) {
			continue
		}
		out = append(out, helpLine{cmd.ID, cmd.Title})
	}
	slices.SortFunc(out, func(a, b helpLine) int { return cmp.Compare(a.what, b.what) })
	return out
}

// commandSections groups the menu bar's own lines the way the bar groups
// them, and puts the keys no menu shows under a heading of their own.
func (a *app) commandSections() []helpSection {
	var out []helpSection
	listed := make(map[string]bool)
	for _, def := range a.menus() {
		s := helpSection{title: def.Title}
		for _, item := range def.Items {
			if item.Command == "" || listed[item.Command] || generatedCommand(item.Command) {
				continue
			}
			cmd, ok := a.root.Commands.Lookup(item.Command)
			if !ok {
				continue
			}
			listed[item.Command] = true
			s.lines = append(s.lines, helpLine{cmd.Title, a.chordFor(cmd.ID)})
		}
		if len(s.lines) > 0 {
			out = append(out, s)
		}
	}

	// Only the ones a key runs. A command with neither a key nor a menu
	// line is reached from a row's plus, where it says what it does.
	rest := helpSection{title: "Keys the menus do not show"}
	for _, cmd := range a.root.Commands.All() {
		if listed[cmd.ID] || generatedCommand(cmd.ID) {
			continue
		}
		if chord := a.chordFor(cmd.ID); chord != "" {
			rest.lines = append(rest.lines, helpLine{cmd.Title, chord})
		}
	}
	if len(rest.lines) > 0 {
		out = append(out, rest)
	}
	return out
}

// generatedCommand reports whether an id was built from the server list,
// the font scan or the shell scan rather than registered by hand.
func generatedCommand(id string) bool {
	for _, prefix := range []string{
		openPrefix, editPrefix, termPrefix, filesPrefix, fontCommandPrefix,
		shellCommandPrefix,
	} {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}

// menus is what the menu bar offers, or nothing when there is no bar.
func (a *app) menus() []ui.MenuDef {
	if a.bar == nil {
		return nil
	}
	return a.bar.Menus
}

// browserHelp is the file browser's key bar, read from the bar's own
// table and spelled the way the bar spells it.
func browserHelp() []helpLine {
	var out []helpLine
	for _, k := range files.BrowserKeys() {
		out = append(out, helpLine{k.Title, k.Shown})
	}
	return out
}

// readerHelp is the file viewer's keys, in the order its bar offers
// them, with the ones that pick text out after them.
//
// The bar cannot say these: it has room for what a file of lines
// offers, and the copy only appears once there is something to copy.
func readerHelp() []helpLine {
	out := make([]helpLine, 0, len(files.ReaderKeys())+5)
	for _, k := range files.ReaderKeys() {
		out = append(out, helpLine{k.Title, k.Shown})
	}
	return append(out,
		helpLine{"Pick text out", "drag, or shift and a key that moves"},
		helpLine{"Pick out the whole file", "ctrl+A"},
		helpLine{"Copy what is picked out", files.CopyKey().Shown},
		helpLine{"Drop what is picked out", "esc"},
	)
}

// chordFor is the chord bound to a command in either keymap, or "" when
// none is.
func (a *app) chordFor(id string) string {
	for _, keys := range a.keymaps() {
		if c, ok := keys.ChordFor(id); ok {
			return c.String()
		}
	}
	return ""
}

// keymaps are the window's two keymaps, Accelerators first and then
// Keys, leaving out either one the window has not built yet.
func (a *app) keymaps() []*ui.Keymap {
	var out []*ui.Keymap
	for _, keys := range []*ui.Keymap{a.root.Accelerators, a.root.Keys} {
		if keys != nil {
			out = append(out, keys)
		}
	}
	return out
}
