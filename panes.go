package main

import (
	"errors"
	"fmt"
	"image/color"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/remote"
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

// newPaneHost names the machine a new pane opens on, for a line that has
// to say where it will go.
func (a *app) newPaneHost() string {
	if m := a.homeMachine(); m != nil {
		return m.at.name
	}
	return conns.Local
}

// homeMachine returns the connection new panes open on, and nil when
// they open a shell on this machine: -ssh named no machine, or the one
// it named has gone.
func (a *app) homeMachine() *machine {
	if a.home == "" {
		return nil
	}
	return a.about(a.home).machine
}

// newTerminal starts another shell where new panes go: on the machine
// -ssh named while that is connected, and on this one otherwise.
func (a *app) newTerminal() (*term.Terminal, error) {
	if m := a.homeMachine(); m != nil {
		return a.terminalOnHome(m)
	}
	return a.localTerminal()
}

// localTerminal starts a shell on the machine gridterm is running on.
func (a *app) localTerminal() (*term.Terminal, error) {
	sess, err := a.newSession(a.lastSize[0], a.lastSize[1])
	if err != nil {
		return nil, fmt.Errorf("start session: %w", err)
	}
	t, err := a.newTerminalOn(sess, conns.Local, conns.Terminal, "")
	if err != nil {
		// The session is ours now and nothing else will close it.
		_ = sess.Close()
		return nil, err
	}
	return t, nil
}

// terminalOnHome starts another shell on the machine -ssh named, riding
// on the connection that is already open to it.
//
// A shell to type into and never the -e command: that command was for
// the pane the window opened with.
func (a *app) terminalOnHome(m *machine) (*term.Terminal, error) {
	sh, err := m.conn.Shell(a.ctx, remote.ShellConfig{
		Cols: a.lastSize[0],
		Rows: a.lastSize[1],
		Term: m.at.term,
	})
	if err != nil {
		return nil, err
	}
	t, err := a.newTerminalOn(sh, m.at.name, conns.Terminal, "")
	if err != nil {
		// The shell is ours now and nothing else will close it.
		_ = sh.Close()
		return nil, err
	}
	// Which connection the pane rides on rather than which machine it is
	// named after, for the reason the on field of machines gives.
	a.machines.runs(t, m)
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
		// A rule around it, and a shadow under it, the way a menu and a
		// dialog have.
		BorderFG: a.colours.ANSI[8],
		ShadowBG: shadow,
	}
	p.SetClipboard(a.pasteText)
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

// newTabs builds the thing that holds several panes and shows one.
//
// It draws no strip of labels: which pane is showing is chosen from the
// sidebar, which has room to say what each one is and which machine it
// is on. A row of names along the top would say the same thing twice,
// and worse.
func (a *app) newTabs(kids ...ui.Widget) *ui.Tabs {
	tb := ui.NewTabs(kids...)
	tb.HideStrip = true
	// It is the window's stage: every pane sits in it, and it has to
	// still be there for the next one when the last is closed.
	tb.Keep = true
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
func (a *app) openTab() error { return a.openTabWith(a.newTerminal) }

// openTabHere is openTab with a shell on this machine, for a window that
// has no connection to open one on.
func (a *app) openTabHere() error { return a.openTabWith(a.localTerminal) }

// openTabWith puts a shell from start in a tab, and closes it again when
// there is nowhere to put it.
func (a *app) openTabWith(start func() (*term.Terminal, error)) error {
	if a.paneToPlaceBeside() == nil && a.stage == nil {
		return errors.New("nothing to open a tab beside")
	}
	next, err := start()
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
// a strip if it is not in one, and putting it straight on the stage when
// the window has no pane at all, which is how -ssh opens.
func (a *app) placeTab(next ui.Widget) error {
	current := a.paneToPlaceBeside()
	if current == nil {
		if a.stage == nil {
			return errors.New("nothing to open a tab beside")
		}
		a.stage.Add(next)
		a.relayout()
		a.focus(next)
		return nil
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
	if w == nil || w == ui.Widget(a.side) || w == ui.Widget(a.panel) {
		return false
	}
	for _, leaf := range ui.Leaves(w) {
		switch leaf.(type) {
		case *term.Terminal, *files.Pane:
		default:
			return false
		}
	}
	return true
}

// closePane takes a pane out of the tree and ends every shell under it.
//
// It works on any container, not just a split, because a pane can sit
// inside anything: asking the tree to detach it is what keeps this from
// having to know.
func (a *app) closePane(w ui.Widget) error { return a.removePane(w, false) }

// removePane takes a pane out of the tree and ends every shell under it.
//
// keep leaves the panel rows behind, greyed and closed and carrying a
// cross to clear them, for a shell of this window's own that ended by
// itself. A pane the user closed takes its row with it.
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

	// Every failure, not the first: a manager with panes on four
	// machines can fail to let go of four of them, and three of those
	// would go unsaid.
	var errs []error
	for _, leaf := range doomed {
		// A file pane is not a terminal: it holds a filesystem, which
		// may be a session on a connection.
		if p, isFiles := leaf.(*files.Pane); isFiles {
			errs = append(errs, a.filesPaneGone(p))
			continue
		}
		t, isTerm := leaf.(*term.Terminal)
		if !isTerm {
			continue
		}
		if e := a.panes[t]; e != nil {
			if keep {
				// The shell went on its own, so the row stays and says
				// so. The pane has gone with it, so there is nothing
				// left to reveal or close and clearing the row is all
				// that is left to do with it.
				e.Meter.Close()
				e.Reveal, e.Close = nil, nil
				e.Clear = a.dropRow(e)
			} else {
				a.registry.Drop(e)
			}
		}
		delete(a.panes, t)
		delete(a.ended, t)
		a.forgetPane(t)
		errs = append(errs, t.Close())
	}
	// Whatever was in front has gone or moved, so the sidebar works it
	// out again rather than holding a pane that is no longer there.
	a.shown = nil
	// The window goes with the last pane. Counted rather than read off
	// an empty tree: the connections panel is a leaf too, so the dock
	// stands in for the pane that went and the tree is never empty.
	if len(a.panes) == 0 && a.files == nil {
		a.quit.Store(true)
	}
	if !detached {
		errs = append(errs, fmt.Errorf("pane was not in the tree"))
	}
	return errors.Join(errs...)
}

// paneEnded deals with a shell that stopped on its own.
//
// A command keeps its pane. What it printed is what it was run for, and
// a window that cleared the screen the moment the program finished would
// take the answer away with it. A shell loses its pane and keeps its
// row: the user asked for a shell rather than for what it last said, and
// the row stays to say the shell has gone until they clear it. A pane
// drawn from a window taken over takes its row with it as well.
func (a *app) paneEnded(t *term.Terminal) error {
	e := a.panes[t]
	if a.windows.drawsFromAWindow(t) {
		// A shell on a window taken over, or a screen watched there,
		// goes without leaving a row: the other window never listed
		// it, and what ended it is on the window's own row.
		return a.removePane(t, false)
	}
	if e == nil || (e.Kind != conns.Command && !a.kept[t]) {
		return a.removePane(t, true)
	}
	if a.ended[t] {
		return nil
	}
	// Marked before the close is tried, so a close that failed is not
	// tried again on every frame for the life of the window.
	a.ended[t] = true
	a.markDirty()
	if err := t.Close(); err != nil {
		// The dot has already gone grey: the stream ended, and that is
		// what closed the meter. So the note is the only place left to
		// say the channel was not let go of, and the caller shows the
		// reason.
		e.Note = "could not be closed"
		return err
	}
	// The channel it was running on is let go of. The pane itself stays,
	// showing what was printed.
	e.Meter.Close()
	return nil
}

// Ended reports whether a pane is one that has stopped and is only being
// read.
func (a *app) Ended(t *term.Terminal) bool { return a.ended[t] }

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
	// The regions too: they are drawn from the same widget tree, and a
	// tree that changed shape has changed theirs as well.
	if a.sideRegion != nil {
		a.sideRegion.g.MarkAllDirty()
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
			if err := a.paneEnded(t); err != nil {
				// Shown rather than logged: a window opened from an icon
				// has no console, and a channel that would not close is
				// worth knowing about.
				a.reportError("A command could not be closed", err)
			}
		}
	}
}

// spot is where a pane the window is opening should go: dividing another
// pane, rather than going in a tab of its own.
//
// It travels with the request rather than being recorded on the window,
// because a connection takes as long as it takes. A pane arriving from
// somewhere else in the meantime would otherwise land in the split that
// was meant for this one.
type spot struct {
	beside ui.Widget
	dir    ui.Dir
}

// place puts a new pane where it was asked to go.
//
// A split that cannot be made falls back to a tab of its own, whatever
// the reason: the pane it was to sit beside may have closed, or gone
// into the background, or the window may have been made too narrow while
// the connection was on its way. The terminal is open either way, and
// losing the login the user waited for because the room for it went
// would be worse than putting it somewhere else.
func (a *app) place(next ui.Widget, at *spot) error {
	if at == nil {
		return a.placeTab(next)
	}
	if err := a.canSplit(at.dir, at.beside, next); err != nil {
		return a.placeTab(next)
	}
	return a.splitWith(at.dir, at.beside, next)
}

// splitWith divides one pane and puts another in the half that opens up.
//
// next may be a pane that is already somewhere else in the tree, which
// is what splitting with an existing tab means: it is taken out of where
// it was first.
//
// A failure leaves next out of the tree, and whoever asked owns it: a
// pane the window has only just made is closed, and one moved from
// somewhere else has to be put back.
func (a *app) splitWith(dir ui.Dir, current, next ui.Widget) error {
	if err := a.canSplit(dir, current, next); err != nil {
		return err
	}

	// Out of wherever it was. A pane the window has only just made is in
	// the tree nowhere, and Detach says so rather than failing.
	if root, moved := ui.Detach(a.root.Widget(), next); moved {
		if root == nil {
			return errors.New("taking the pane out of the tree left nothing")
		}
		if root != a.root.Widget() {
			a.root.SetWidget(root)
		}
	}

	split := a.newSplit(dir, current, next)
	parent := ui.ParentOf(a.root.Widget(), current)
	if parent == nil {
		a.root.SetWidget(split)
	} else if !parent.Replace(current, split) {
		return fmt.Errorf("%T would not take a split in place of the pane", parent)
	}
	// The tree changed shape, so every pane has to be told its new size
	// before the new one takes focus.
	a.relayout()
	a.focus(next)
	return nil
}

// canDivide says why a pane cannot be divided, or nil when it can.
//
// Asked before the user is, so a split that could never be made is not
// a question first.
func (a *app) canDivide(dir ui.Dir, current ui.Widget) error {
	switch {
	case current == nil:
		return errors.New("nothing to split")
	case !a.isPane(current):
		return errors.New("that is not a pane to split")
	case !a.roomToSplit(current, dir):
		// A pane too small to divide would leave two panes nobody can
		// see, each with a live shell still in the focus cycle.
		return errors.New("no room to split")
	}
	if _, ok := ui.ParentOf(a.root.Widget(), current).(*files.Browser); ok {
		// The file manager holds panes of its own and nothing else, so
		// it would refuse the split after the other pane had already
		// been taken out of the tree for it.
		return errors.New(
			"a file pane cannot be split: add a pane to the file manager instead")
	}
	return nil
}

// canSplit says why one pane cannot be divided with another, or nil
// when it can.
//
// Asked before anything moves, so a split that cannot be made leaves the
// tree exactly as it was.
func (a *app) canSplit(dir ui.Dir, current, next ui.Widget) error {
	switch {
	case next == nil:
		return errors.New("nothing to split with")
	case current == next:
		return errors.New("a pane cannot be split with itself")
	case ui.ParentOf(next, current) != nil:
		return errors.New("a pane cannot be split with what it is inside")
	}
	if _, ok := ui.ParentOf(a.root.Widget(), next).(*files.Browser); ok {
		// A file pane belongs to the manager. Taken out of it, it loses
		// every key it has -- Tab, copy, rename, the lot live on the
		// manager -- and a manager left with none is detached with the
		// window still holding it.
		return errors.New("a file pane belongs to the file manager and cannot be moved out of it")
	}
	return a.canDivide(dir, current)
}

// live reports whether a widget is still a pane the window is showing.
//
// A closed pane and one the window has only just made are both outside
// the tree, and ui.Detach cannot tell them apart. Anything holding a
// pane across time -- a chooser waiting to be answered, a connection on
// its way -- has to ask before it uses one.
func (a *app) live(w ui.Widget) bool {
	return w != nil && a.isPane(w) && ui.ParentOf(a.root.Widget(), w) != nil
}

// unsplitFocused takes the focused pane out of the split it is in and
// gives it a tab of its own.
//
// Nothing is closed. The pane beside it takes the whole of the room the
// split had, which is what undoing a split means.
func (a *app) unsplitFocused() error {
	w := ui.FocusedLeaf(a.root.Widget())
	if !a.isPane(w) {
		return errors.New("there is no pane here to take out of a split")
	}
	if a.stage == nil {
		return errors.New("there is nowhere to put it")
	}
	if _, ok := ui.ParentOf(a.root.Widget(), w).(*ui.Split); !ok {
		return errors.New("this pane is not in a split")
	}
	root, detached := ui.Detach(a.root.Widget(), w)
	if !detached {
		return errors.New("the pane could not be taken out of its split")
	}
	if root != nil && root != a.root.Widget() {
		a.root.SetWidget(root)
	}
	a.stage.Add(w)
	a.relayout()
	a.focus(w)
	return nil
}
