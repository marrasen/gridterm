package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
)

// scrollbackCommand opens a pane's scrollback in the file viewer, and
// scrollbackTitle is what the menu and the palette call it.
const (
	scrollbackCommand = "pane.scrollback"
	scrollbackTitle   = "Search this pane's scrollback"
)

// showScrollback opens the focused pane's screen and the scrollback
// behind it in a file viewer.
//
// A terminal cannot be searched: it is a grid of cells that a program
// is still drawing on. The viewer can, and already has the find key,
// the line numbers and the selection, so the cheapest way to search a
// pane is to hand its text to the thing that reads files.
//
// The text is taken as it stands. Ctrl+R in the viewer takes it again,
// so a pane that has said more since can be caught up with.
func (a *app) showScrollback() error {
	t := a.focusedTerminal()
	if t == nil {
		return errors.New("the pane in front is not a terminal, so it has no scrollback")
	}
	if a.readers == nil {
		a.readers = map[*files.Reader]*reader{}
	}
	if r := a.scrollbackOf(t); r != nil {
		// One viewer per pane. A second would show the same text, and
		// the first is already where the user left it.
		a.focus(r)
		return nil
	}

	r := files.NewReader(scrollbackName(a.paneName(t)), "")
	r.Style = a.paneStyle()
	r.Read = func(then func([]string, bool, error)) {
		then(scrollbackLines(t), false, nil)
	}
	r.OnCopy = a.clip.set
	// Saved on this machine, because this is where the user is looking.
	// A pane on a machine far away still writes its scrollback here.
	r.SaveAs = suggestedSavePath(r.Name())
	r.OnSave = saveLines
	r.OnClose = func() {
		a.pump.post(func() {
			if err := a.closePane(r); err != nil {
				a.reportError("Could not close the scrollback", err)
			}
		})
	}
	if err := a.placePane(r); err != nil {
		return err
	}
	host := conns.Local
	if e := a.panes[t]; e != nil {
		host = e.Host
	}
	row := &conns.Entry{
		Host:   host,
		Kind:   conns.Reader,
		Label:  r.Name(),
		Reveal: func() { a.focus(r) },
		Close:  func() error { return a.closePane(r) },
	}
	a.readers[r] = &reader{row: row, pane: t}
	a.registry.Add(row)
	r.Open()
	return nil
}

// scrollbackOf is the viewer already open on a pane's scrollback, or
// nil when there is none.
func (a *app) scrollbackOf(t *term.Terminal) *files.Reader {
	for r, held := range a.readers {
		if held.pane == t {
			return r
		}
	}
	return nil
}

// scrollbackLines is a pane's screen and the scrollback behind it, as
// the lines a viewer shows.
//
// Plain text, not the colours: the viewer draws a file in one style,
// and what this is for is finding a line rather than admiring it.
func scrollbackLines(t *term.Terminal) []string {
	text := t.TextLines(t.LinesHeld())
	if text == "" {
		return nil
	}
	// The pane ends in a newline when its last row is blank, and that
	// is not a line: it is what comes after the last one.
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// scrollbackName is what the viewer and its sidebar row are called.
func scrollbackName(pane string) string {
	if pane == "" {
		return "scrollback"
	}
	return pane + " scrollback"
}

// suggestedSavePath is where Ctrl+S offers to write a pane's
// scrollback: the user's own directory, under a name taken from the
// pane, with the characters a filesystem will not take swapped out.
func suggestedSavePath(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Nowhere better to suggest. The user types a path instead,
		// which is what the question is for.
		home = ""
	}
	return filepath.Join(home, safeFileName(name)+".txt")
}

// safeFileName turns a pane's name into something a filesystem takes.
func safeFileName(name string) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < ' ' {
			return '-'
		}
		return r
	}, name)
	clean = strings.Trim(clean, " .-")
	if clean == "" {
		return "scrollback"
	}
	return clean
}

// saveLines writes lines to a file on this machine, one per line, with
// a newline after the last.
//
// Written to a file of its own and renamed into place, so a write that
// fails part way leaves what was there before rather than half of this.
func saveLines(at string, lines []string) error {
	if at == "" {
		return errors.New("no path to save to")
	}
	dir := filepath.Dir(at)
	f, err := os.CreateTemp(dir, filepath.Base(at)+"-*")
	if err != nil {
		return fmt.Errorf("could not write in %s: %w", dir, err)
	}
	tmp := f.Name()
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if _, err := f.WriteString(b.String()); err != nil {
		return fmt.Errorf("could not write %s: %w", at, errors.Join(err, f.Close(), os.Remove(tmp)))
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("could not write %s: %w", at, errors.Join(err, os.Remove(tmp)))
	}
	if err := os.Rename(tmp, at); err != nil {
		return fmt.Errorf("could not put it at %s: %w", at, errors.Join(err, os.Remove(tmp)))
	}
	return nil
}
