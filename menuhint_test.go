package main

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// bottomRow is what the last row of the window says.
func bottomRow(t *testing.T, a *testApp) string {
	t.Helper()
	cols, rows := a.g.Size()
	var b strings.Builder
	for x := range cols {
		c := a.g.At(x, rows-1)
		if c.Rune == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(c.Rune)
	}
	return strings.TrimRight(b.String(), " ")
}

// The menus say the short half of a title, and the whole of it goes
// along the bottom row while a menu is open.
func TestTheBottomRowSaysWhatTheMenuLineDoes(t *testing.T) {
	a := newTestApp(t, 90, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	withMenubar(t, a)

	m := openMenuWith(t, a, "pane.splitRight")
	selectMenuItem(t, m, "pane.splitRight")
	a.relayout()
	a.root.Draw(a.g.View())
	a.drawHint()

	// The menu says "Right…" under a "Split" header; the row says the
	// whole of it.
	if got := barMenuLine(t, a, "pane.splitRight"); got != "Right…" {
		t.Errorf("the menu line reads %q", got)
	}
	if got := bottomRow(t, a); !strings.Contains(got, "Split Right") {
		t.Errorf("the bottom row reads %q, want what the line does", got)
	}
}

// With no menu open the row is the pane's own again.
func TestWithNoMenuOpenTheBottomRowIsThePanes(t *testing.T) {
	a := newTestApp(t, 90, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	withMenubar(t, a)
	a.relayout()

	if got := a.bar.Hint(); got != "" {
		t.Errorf("the bar offers the hint %q with no menu open", got)
	}
	was := drawnBottom(t, a)
	a.drawHint()
	if got := bottomRow(t, a); got != was {
		t.Errorf("the bottom row became %q, want the pane's own %q", got, was)
	}
}

// drawnBottom is the bottom row with the tree drawn and no hint over
// it.
func drawnBottom(t *testing.T, a *testApp) string {
	t.Helper()
	a.root.Draw(a.g.View())
	return bottomRow(t, a)
}

// A header is a caption over the group under it, and cannot be
// chosen: stepping down a menu goes from the line above it to the
// line below.
func TestAHeaderCannotBeChosen(t *testing.T) {
	a := newTestApp(t, 90, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	withMenubar(t, a)

	m := openMenuWith(t, a, "pane.splitRight")

	var headers int
	for _, item := range m.Items() {
		if item.Header != "" {
			headers++
		}
	}
	if headers == 0 {
		t.Fatal("the Pane menu has no headers on it")
	}
	// Every line the menu will stop on names a command.
	for range m.Items() {
		cmd, ok := m.Selected()
		if !ok || cmd.ID == "" {
			t.Fatal("the menu stopped on a line that runs nothing")
		}
		if _, err := m.HandleKey(press(input.KeyDown, 0)); err != nil {
			t.Fatalf("down the menu: %v", err)
		}
	}
}

// A switch draws a tick when it is on, and none when it is off.
func TestASwitchDrawsATick(t *testing.T) {
	a := newTestApp(t, 90, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	withMenubar(t, a)
	if err := a.shellSetup.set(true); err != nil {
		t.Fatalf("turn it on: %v", err)
	}

	on := menuDrawn(t, a, "shell.setup")
	if !strings.Contains(on, "✓") {
		t.Errorf("a switch that is on drew no tick:\n%s", on)
	}

	if err := a.shellSetup.set(false); err != nil {
		t.Fatalf("turn it off: %v", err)
	}

	if off := menuDrawn(t, a, "shell.setup"); strings.Contains(off, "✓") {
		t.Errorf("a switch that is off drew a tick:\n%s", off)
	}
}

// menuDrawn paints the menu holding a command and answers what it
// says.
func menuDrawn(t *testing.T, a *testApp, id string) string {
	t.Helper()
	// Whatever was open goes first: the bar walks along from wherever
	// it is, and one already open would send it the wrong way.
	a.bar.Close()
	m := openMenuWith(t, a, id)
	g := grid.New(60, 30, a.colours.FG, a.colours.BG)
	m.Layout(ui.Size{Cols: 60, Rows: 30})
	m.Draw(g.View())
	var b strings.Builder
	cols, rows := g.Size()
	for y := range rows {
		for x := range cols {
			if c := g.At(x, y); c.Rune != 0 {
				b.WriteRune(c.Rune)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
