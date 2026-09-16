package main

import (
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
	a.showNotice(helpTitle, a.helpText(), false)
	return nil
}

// helpText is the list itself: the commands under the menu each hangs
// on, then the ones no menu offers, then the file browser's own keys.
//
// Read from the registry and the keymaps rather than written out, so it
// cannot drift from what the keys do.
func (a *app) helpText() string {
	sections := a.commandSections()
	sections = append(sections, helpSection{
		title: "The file browser's keys",
		lines: browserHelp(),
	})

	// One column across the whole list, so the chords line up from the
	// top of the dialog to the bottom.
	width := 0
	for _, s := range sections {
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
		for _, l := range s.lines {
			out = append(out, "  "+l.write(width))
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

// helpSection is one heading of the list and the lines under it.
type helpSection struct {
	title string
	lines []helpLine
}

// commandSections groups the registered commands the way the menu bar
// groups them, and puts whatever no menu offers under its own heading.
func (a *app) commandSections() []helpSection {
	var out []helpSection
	listed := make(map[string]bool)
	for _, def := range a.menus() {
		s := helpSection{title: def.Title}
		for _, item := range def.Items {
			if item.Command == "" || listed[item.Command] {
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

	// The commands the menus leave out: the ones only a key, the palette
	// or a row's plus reaches.
	rest := helpSection{title: "Other commands"}
	for _, cmd := range a.root.Commands.All() {
		if listed[cmd.ID] {
			continue
		}
		rest.lines = append(rest.lines, helpLine{cmd.Title, a.chordFor(cmd.ID)})
	}
	if len(rest.lines) > 0 {
		out = append(out, rest)
	}
	return out
}

// menus is what the menu bar offers, or nothing when there is no bar.
func (a *app) menus() []ui.MenuDef {
	if a.bar == nil {
		return nil
	}
	return a.bar.Menus
}

// browserHelp is the file browser's key bar, read from the bar's own
// table so the two say the same thing.
func browserHelp() []helpLine {
	var out []helpLine
	for _, k := range files.BrowserKeys() {
		out = append(out, helpLine{k.Title, k.Chord.String()})
	}
	return out
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

// keymaps are the window's two keymaps, in the order a key press is
// offered to them, leaving out any the window has not built yet.
func (a *app) keymaps() []*ui.Keymap {
	var out []*ui.Keymap
	for _, keys := range []*ui.Keymap{a.root.Accelerators, a.root.Keys} {
		if keys != nil {
			out = append(out, keys)
		}
	}
	return out
}
