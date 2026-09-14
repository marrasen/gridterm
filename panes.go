package main

import (
	"errors"
	"fmt"
	"image/color"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
)

// focusedTerminal returns the terminal keys are reaching, or nil when
// the focused pane is something else.
func (a *app) focusedTerminal() *term.Terminal {
	t, _ := ui.FocusedLeaf(a.root.Widget()).(*term.Terminal)
	return t
}

// newTerminal starts another shell, configured like the first.
func (a *app) newTerminal() (*term.Terminal, error) {
	sess, err := a.newSession(a.lastSize[0], a.lastSize[1])
	if err != nil {
		return nil, fmt.Errorf("start session: %w", err)
	}
	t, err := a.newTerminalOn(sess, a.localHost, conns.Terminal, "")
	if err != nil {
		// The session is ours now and nothing else will close it.
		_ = sess.Close()
		return nil, err
	}
	return t, nil
}

// openPalette shows the command dialog, or closes it when it is already
// open: a key that opens something is expected to close it again.
func (a *app) openPalette() error {
	if a.palette != nil {
		a.closePalette()
		return nil
	}
	p := ui.NewPalette(a.root.Commands, a.root.Accelerators, a.closePalette)
	p.Style = ui.PaletteStyle{
		FG: a.colours.FG,
		// No background of its own: the frosted panel behind the dialog
		// is the background, and an opaque fill would hide it.
		BG: color.RGBA{},
		// Yellow, because the letters the query found have to read as
		// found. The window's own foreground would only differ from the
		// rest of the title by weight.
		MatchFG:    a.colours.ANSI[3],
		SelectedFG: a.colours.BG,
		SelectedBG: a.colours.FG,
		// Dimmer than the title: a key binding is a note beside the
		// command, not part of its name.
		ChordFG: a.colours.ANSI[8],
	}
	p.SetClipboard(clipboardRead)
	a.palette = p
	a.dismissPalette = a.showModal(p, func() { a.palette, a.dismissPalette = nil, nil })
	return nil
}

// closePalette takes the dialog and its layer away.
func (a *app) closePalette() {
	if a.dismissPalette != nil {
		a.dismissPalette()
	}
}

// newTabs builds a tab strip carrying the window's colours.
func (a *app) newTabs(kids ...ui.Widget) *ui.Tabs {
	tb := ui.NewTabs(kids...)
	tb.StripBG = a.colours.BG
	tb.InactiveFG = a.colours.FG
	tb.ActiveFG = a.colours.BG
	tb.ActiveBG = a.colours.FG
	return tb
}

// stripAbove returns the nearest tab strip containing w, along with the
// tab of that strip that w sits inside.
//
// It walks rather than asking for w's own parent: a pane that has been
// split is a grandchild of the strip, and the strip is still the one its
// tab keys mean.
func (a *app) stripAbove(w ui.Widget) (*ui.Tabs, ui.Widget) {
	for child := w; child != nil; {
		parent := ui.ParentOf(a.root.Widget(), child)
		if parent == nil {
			return nil, nil
		}
		if strip, ok := parent.(*ui.Tabs); ok {
			return strip, child
		}
		child = parent
	}
	return nil, nil
}

// paneToPlaceBeside returns the pane a new one goes next to: the focused
// one when it is a pane, and otherwise whichever pane has the focus
// inside the rest of the window.
//
// The connections panel is a leaf of the tree like a terminal is, so
// without this a new tab opened while the panel had the keys would build
// a tab strip around the panel and put the connections list in a tab.
func (a *app) paneToPlaceBeside() ui.Widget {
	if w := ui.FocusedLeaf(a.root.Widget()); a.isPane(w) {
		return w
	}
	if a.dock != nil {
		if w := ui.FocusedLeaf(a.dock.Rest()); a.isPane(w) {
			return w
		}
	}
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if a.isPane(leaf) {
			return leaf
		}
	}
	return nil
}

// openTab puts a new shell in the strip holding the focused pane,
// starting a strip if it is not in one.
func (a *app) openTab() error {
	if a.paneToPlaceBeside() == nil {
		return errors.New("nothing to open a tab beside")
	}
	next, err := a.newTerminal()
	if err != nil {
		return err
	}
	if err := a.placeTab(next); err != nil {
		delete(a.panes, next)
		_ = next.Close()
		return err
	}
	a.showPane(next)
	return nil
}

// placeTab puts a widget in the strip holding the focused pane, starting
// a strip if it is not in one.
func (a *app) placeTab(next ui.Widget) error {
	current := a.paneToPlaceBeside()
	if current == nil {
		return errors.New("nothing to open a tab beside")
	}
	if strip, _ := a.stripAbove(current); strip != nil {
		strip.Add(next)
	} else {
		strip := a.newTabs(current, next)
		if parent := ui.ParentOf(a.root.Widget(), current); parent != nil {
			parent.Replace(current, strip)
		} else {
			a.root.SetWidget(strip)
		}
		strip.Focus(next)
	}
	a.relayout()
	a.focus(next)
	return nil
}

// focusTab moves n tabs along in the strip holding the focused pane,
// wrapping at the ends. It does nothing when the pane is not in a strip.
func (a *app) focusTab(n int) error {
	strip, mine := a.stripAbove(ui.FocusedLeaf(a.root.Widget()))
	if strip == nil {
		return nil
	}
	tabs := strip.Children()
	if len(tabs) < 2 {
		return nil
	}
	at := 0
	for i, tab := range tabs {
		if tab == mine {
			at = i
			break
		}
	}
	// Go's % keeps the sign of the dividend, so a step backwards from
	// the first tab needs the extra turn to land on the last.
	next := ((at+n)%len(tabs) + len(tabs)) % len(tabs)
	a.focus(ui.FocusedLeaf(tabs[next]))
	return nil
}

// newSplit builds a split carrying the window's divider colours.
func (a *app) newSplit(dir ui.Dir, first, second ui.Widget) *ui.Split {
	s := ui.NewSplit(dir, first, second)
	s.DividerFG = a.colours.FG
	s.DividerBG = a.colours.BG
	return s
}

// splitFocused puts a new shell beside the focused pane.
func (a *app) splitFocused(dir ui.Dir) error {
	current := a.paneToPlaceBeside()
	if current == nil {
		return errors.New("nothing to split")
	}
	// A pane too small to divide would leave two panes nobody can see,
	// each with a live shell still in the focus cycle.
	if !a.roomToSplit(current, dir) {
		return errors.New("no room to split")
	}
	next, err := a.newTerminal()
	if err != nil {
		return err
	}

	split := a.newSplit(dir, current, next)
	if parent := ui.ParentOf(a.root.Widget(), current); parent != nil {
		parent.Replace(current, split)
	} else {
		a.root.SetWidget(split)
	}
	// The tree changed shape, so every pane has to be told its new size
	// before the new one takes focus.
	a.relayout()
	a.focus(next)
	a.showPane(next)
	return nil
}

// closeFocused shuts the focused pane and gives its room to whatever
// shared the split. Closing the last pane closes the window.
func (a *app) closeFocused() error {
	return a.closePane(ui.FocusedLeaf(a.root.Widget()))
}

// isPane reports whether a widget is something closePane may take out.
//
// The connections panel is a leaf of the tree like a terminal is, so
// without this the close-pane key would detach the panel and leave the
// window with no way to get it back.
func (a *app) isPane(w ui.Widget) bool {
	if w == nil || w == ui.Widget(a.panel) {
		return false
	}
	for _, leaf := range ui.Leaves(w) {
		switch leaf.(type) {
		case *term.Terminal:
		case *files.Browser:
			// A file browser is one pane with two sides rather than a
			// container the tree may take apart: neither side means
			// anything without the other.
		default:
			return false
		}
	}
	return true
}

// browsersIn returns the file browsers inside a widget, for one that is
// being taken out of the tree.
func (a *app) browsersIn(w ui.Widget) []*browser {
	var found []*browser
	for at, b := range a.browsers {
		if at == w || ui.ParentOf(w, at) != nil {
			found = append(found, b)
		}
	}
	return found
}

// closePane takes a pane out of the tree and ends every shell under it.
//
// It works on any container, not just a split, because a pane can sit
// inside anything: asking the tree to detach it is what keeps this from
// having to know.
func (a *app) closePane(w ui.Widget) error { return a.removePane(w, false) }

// removePane takes a pane out of the tree and ends every shell under it.
//
// keep leaves the panel rows behind, greyed and closed, for a shell that
// ended on its own. A pane the user closed takes its row with it: they
// know what happened to it.
func (a *app) removePane(w ui.Widget, keep bool) error {
	if w == nil {
		return nil
	}
	if !a.isPane(w) {
		// The connections panel, or something else that is not a pane.
		// Detaching it would take it out of the tree for good.
		return nil
	}
	// Every shell under it, in case the pane being closed is a whole
	// subtree rather than one terminal.
	doomed := ui.Leaves(w)

	root, detached := ui.Detach(a.root.Widget(), w)
	switch {
	case !detached:
		// The tree surgery failed, but the shells are still ours to end.
		// Leaving them running is worse than a crooked tree.
	case root == nil:
		// Nothing left in the tree at all, which is what closing the
		// last pane looks like when there is no panel beside it.
	default:
		if root != a.root.Widget() {
			a.root.SetWidget(root)
		}
		a.relayout()
		a.focus(ui.FocusedLeaf(a.root.Widget()))
	}

	var err error
	// A browser is not a terminal: it holds two filesystems, and one of
	// them may be a session on a connection.
	for _, b := range a.browsersIn(w) {
		if cerr := a.closeBrowser(b.view); cerr != nil && err == nil {
			err = cerr
		}
	}
	for _, leaf := range doomed {
		t, isTerm := leaf.(*term.Terminal)
		if !isTerm {
			continue
		}
		if e := a.panes[t]; e != nil {
			if keep {
				// The shell went on its own, so the row stays and says
				// so: what a command did after it stopped is worth
				// reading. There is nothing left to reveal or close.
				e.Meter.Close()
				e.Reveal, e.Close = nil, nil
			} else {
				a.registry.Drop(e)
			}
		}
		delete(a.panes, t)
		a.forgetPane(t)
		if cerr := t.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	// The window goes with the last pane. Counted rather than read off
	// an empty tree: the connections panel is a leaf too, so the dock
	// stands in for the pane that went and the tree is never empty.
	if len(a.panes) == 0 && len(a.browsers) == 0 {
		a.quit.Store(true)
	}
	if !detached && err == nil {
		err = fmt.Errorf("pane was not in the tree")
	}
	return err
}

// roomToSplit reports whether a pane has the cells to become two, which
// needs one each side and one for the divider.
//
// The size comes from the tree rather than the pane, because a pane with
// no room to be shown keeps the last size it was given.
func (a *app) roomToSplit(w ui.Widget, dir ui.Dir) bool {
	area, shown := a.root.AreaOf(w)
	if !shown {
		return false
	}
	along := area.Cols
	if dir == ui.Rows {
		along = area.Rows
	}
	return along >= 3
}

// focusPane moves focus n panes along, wrapping at the ends.
func (a *app) focusPane(n int) error {
	panes := ui.Leaves(a.root.Widget())
	if len(panes) < 2 {
		return nil
	}
	current := ui.FocusedLeaf(a.root.Widget())
	at := 0
	for i, p := range panes {
		if p == current {
			at = i
			break
		}
	}
	// Go's % keeps the sign of the dividend, so a step backwards from
	// the first pane needs the extra turn to land on the last.
	next := ((at+n)%len(panes) + len(panes)) % len(panes)
	a.focus(panes[next])
	return nil
}

// focus points the whole chain of splits at one pane.
func (a *app) focus(target ui.Widget) {
	if target == nil {
		return
	}
	// Each container from the root down has to select the child holding
	// the target, or focus stops at the first one pointing elsewhere.
	for w := target; ; {
		parent := ui.ParentOf(a.root.Widget(), w)
		if parent == nil {
			break
		}
		parent.Focus(w)
		w = parent
	}
	a.markDirty()
}

// relayout re-measures the tree after its shape changed.
func (a *app) relayout() {
	a.root.Layout(ui.Rect{Cols: a.lastSize[0], Rows: a.lastSize[1]})
	a.markDirty()
}

// markDirty repaints everything, for a change the grid cannot see: a
// pane moving, appearing or going away.
func (a *app) markDirty() {
	if a.g != nil {
		a.g.MarkAllDirty()
	}
}

// paneExited is called from a shell's own goroutine when it goes.
//
// The notice is dropped rather than waited on. Nothing drains the queue
// once the window is closing, and a notice only asks for a rescan that
// another one will ask for anyway.
func (a *app) paneExited() {
	select {
	case a.exits <- struct{}{}:
	default:
	}
}

// reapExited closes panes whose shell has gone, from the drawing
// goroutine where the tree may be touched.
func (a *app) reapExited() {
	for {
		select {
		case <-a.exits:
		default:
			return
		}
		for t := range a.panes {
			if !t.Exited() {
				continue
			}
			if err := a.removePane(t, true); err != nil {
				a.logError(err)
			}
		}
	}
}
