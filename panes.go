package main

import (
	"cmp"
	"errors"
	"fmt"
	"image/color"
	"os"
	"slices"
	"strings"

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
	return a.localTerminalOn(a.localShell())
}

// localTerminalOn starts a pane here on argv. A nil argv leaves the
// shell to session.StartLocal.
func (a *app) localTerminalOn(argv []string) (*term.Terminal, error) {
	sess, err := a.newShell(argv, "", a.lastSize[0], a.lastSize[1])
	if err != nil {
		return nil, fmt.Errorf("start session: %w", err)
	}
	t, err := a.newTerminalOn(sess, conns.Local, conns.Terminal, "")
	if err != nil {
		// The session is ours now and nothing else will close it.
		_ = sess.Close()
		return nil, err
	}
	a.startsAgain(t, argv, "", nil)
	return t, nil
}

// runCommandHere opens a pane running a command on this machine, at a
// spot or on the stage. dir is where it runs, and empty is wherever the
// shell lands. The pane gets a command row, so it is named by what it
// runs and offers to run it again when it ends.
func (a *app) runCommandHere(argv []string, dir string, at *spot) error {
	sess, err := a.newShell(argv, dir, a.lastSize[0], a.lastSize[1])
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	t, err := a.openSessionPane(sess, conns.Local, conns.Command, labelFor(argv), at)
	if err != nil {
		// Ours unless openSessionTab closed it already, and closing
		// twice is safe.
		_ = sess.Close()
		return err
	}
	a.startsAgain(t, argv, dir, nil)
	return nil
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
	// A shell to type into, which is what this opens and what starting
	// it again in the pane would open.
	a.startsAgain(t, nil, "", m)
	return t, nil
}

// localArgv is the argv a pane on this machine runs, with the default
// shell filled in: session.StartLocal picks COMSPEC when it is handed
// nothing, and a row has to be able to name what that is.
func (a *app) localArgv(t *term.Terminal) []string {
	if e := a.panes[t]; e == nil || e.Host != conns.Local {
		return nil
	}
	if s := a.started[t]; s != nil && len(s.argv) > 0 {
		return s.argv
	}
	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		return []string{comspec}
	}
	return []string{"cmd.exe"}
}

// shellName is what a pane's row calls the shell it runs, and empty when
// this machine has no name for it. A command row is named by what it
// runs, even when what it runs is a shell.
func (a *app) shellName(t *term.Terminal) string {
	if e := a.panes[t]; e == nil || e.Kind != conns.Terminal {
		return ""
	}
	argv := a.localArgv(t)
	if len(argv) == 0 {
		return ""
	}
	sh, ok := a.shellPick.running(argv)
	if !ok {
		return ""
	}
	return sh.Title
}

// namesItself reports whether what a pane called its window is only the
// path of the program it is running, which says nothing its row does not.
func (a *app) namesItself(t *term.Terminal, title string) bool {
	argv := a.localArgv(t)
	return len(argv) > 0 && strings.EqualFold(title, argv[0])
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

// newDeck builds the thing that holds several panes and shows one.
//
// Which pane is showing is chosen from the sidebar, which has room to
// say what each one is and which machine it is on.
func (a *app) newDeck(kids ...ui.Widget) *ui.Deck {
	tb := ui.NewDeck(kids...)
	// It is the window's stage: every pane sits in it, and it has to
	// still be there for the next one when the last is closed.
	tb.Keep = true
	return tb
}

// deckAbove returns the nearest deck holding w.
//
// It walks rather than asking for w's own parent: a pane that has been
// split is a grandchild of the deck, and the deck is still the one a new
// pane joins.
func (a *app) deckAbove(w ui.Widget) *ui.Deck {
	for child := w; child != nil; {
		parent := ui.ParentOf(a.root.Widget(), child)
		if parent == nil {
			return nil
		}
		if deck, ok := parent.(*ui.Deck); ok {
			return deck
		}
		child = parent
	}
	return nil
}

// paneToPlaceBeside returns the pane a new one goes next to: the focused
// one when it is a pane, and otherwise whichever pane has the focus
// inside the rest of the window.
//
// The connections panel is a leaf of the tree like a terminal is, so
// without this a new pane opened while the panel had the keys would put
// the connections list in the deck.
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

// openPane puts a new shell beside the focused pane.
func (a *app) openPane() error { return a.openPaneWith(a.newTerminal) }

// openPaneHere is openPane with a shell on this machine, for a window
// that has no connection to open one on.
func (a *app) openPaneHere() error { return a.openPaneWith(a.localTerminal) }

// openPaneWith puts a shell from start in a pane, and closes it again
// when there is nowhere to put it.
func (a *app) openPaneWith(start func() (*term.Terminal, error)) error {
	if a.paneToPlaceBeside() == nil && a.stage == nil {
		return errors.New("nothing to open a pane beside")
	}
	next, err := start()
	if err != nil {
		return err
	}
	if err := a.placePane(next); err != nil {
		delete(a.panes, next)
		delete(a.started, next)
		_ = next.Close()
		return err
	}
	a.showPane(next)
	return nil
}

// placePane puts a widget beside the focused pane, and straight on the
// stage when the window has no pane at all, which is how -ssh opens.
func (a *app) placePane(next ui.Widget) error {
	// The deck holding the focused pane, and the stage when there is no
	// pane to go beside.
	holder := a.stage
	if current := a.paneToPlaceBeside(); current != nil {
		if above := a.deckAbove(current); above != nil {
			holder = above
		}
	}
	if holder == nil {
		return errors.New("nothing to open a pane beside")
	}
	holder.Add(next)
	a.relayout()
	a.focus(next)
	return nil
}

// focusInSidebarOrder moves focus n panes along the order the sidebar
// lists them in, wrapping at the ends.
func (a *app) focusInSidebarOrder(n int) error {
	stepFocus(a.panesInSidebarOrder(), ui.FocusedLeaf(a.root.Widget()), n, a.focus)
	return nil
}

// panesInSidebarOrder is the window's panes in the order the sidebar
// lists them: down the rows and across the machine headings.
func (a *app) panesInSidebarOrder() []ui.Widget {
	var here []ui.Widget
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if a.isPane(leaf) {
			here = append(here, leaf)
		}
	}
	if a.panel == nil || len(here) < 2 {
		return here
	}

	// Which row each connection is on.
	rows := a.panel.Rows()
	rank := make(map[*conns.Entry]int, len(rows))
	for i, row := range rows {
		if e, ok := row.Key.(*conns.Entry); ok {
			rank[e] = i
		}
	}
	// A pane the sidebar has not named sorts last, so a collapsed
	// sidebar cannot strand it.
	place := func(w ui.Widget) int {
		if i, ok := rank[a.entryOf(w)]; ok {
			return i
		}
		return len(rows)
	}
	slices.SortStableFunc(here, func(x, y ui.Widget) int {
		return cmp.Compare(place(x), place(y))
	})
	return here
}

// entryOf is the sidebar row a pane is listed on, and nil for a widget
// that is not a pane.
func (a *app) entryOf(w ui.Widget) *conns.Entry {
	switch p := w.(type) {
	case *term.Terminal:
		return a.panes[p]
	case *files.Pane:
		if a.files == nil {
			return nil
		}
		return a.files.rows[p]
	}
	return nil
}

// stepFocus focuses the pane n along from current, wrapping at the ends.
func stepFocus(panes []ui.Widget, current ui.Widget, n int, focus func(ui.Widget)) {
	if len(panes) == 0 {
		return
	}
	at := -1
	for i, p := range panes {
		if p == current {
			at = i
			break
		}
	}
	if at < 0 {
		// The keys are on something that is not a pane, the sidebar
		// most likely, so the step comes in at an end.
		if n < 0 {
			focus(panes[len(panes)-1])
		} else {
			focus(panes[0])
		}
		return
	}
	if len(panes) < 2 {
		return
	}
	// Go's % keeps the sign of the dividend, so a step backwards from
	// the first pane needs the extra turn to land on the last.
	focus(panes[((at+n)%len(panes)+len(panes))%len(panes)])
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
//
// A file session that ran its own bound out on the way is logged rather
// than shown: the pane has gone either way.
func (a *app) closeFocused() error {
	return a.graceLogged(a.closePane(ui.FocusedLeaf(a.root.Widget())))
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

// closePane takes a pane out of the tree, ends every shell under it and
// takes its row off the panel.
//
// It works on any container, not just a split, because a pane can sit
// inside anything: asking the tree to detach it is what keeps this from
// having to know.
//
// This is the only way a pane goes. A shell that ends leaves the pane
// where it is, so closing one is always the user saying they are done
// with it: the close key, the row's own close, or Close on the question
// the last row asks.
func (a *app) closePane(w ui.Widget) error {
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
		// Whichever kind it is, the order panes were last used in lets
		// go of it: that list would otherwise hold a pane nobody can
		// reach, and its scrollback with it.
		a.forgetRecent(leaf)
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
			a.registry.Drop(e)
		}
		delete(a.panes, t)
		delete(a.ended, t)
		delete(a.started, t)
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

// paneEnded deals with a program that stopped on its own.
//
// The pane stays, whatever was in it and however it ended. What it
// printed is still worth reading, scrollback and all, and a server that
// was rebooted leaves a pane that says what happened before it went. The
// row goes grey to say the program has finished, the last row asks what
// to do next, and the pane goes only when the user answers Close.
func (a *app) paneEnded(t *term.Terminal) error {
	e := a.panes[t]
	if e == nil {
		// No row, so there is no way left to reach the pane and nothing
		// to say the program has gone.
		return a.closePane(t)
	}
	if a.ended[t] {
		// The status can land after the program went, and the question
		// exists to say it.
		a.askAgainIfMoreIsKnown(t)
		return nil
	}
	// Marked before the close is tried, so a close that failed is not
	// tried again on every frame for the life of the window.
	a.ended[t] = true
	a.markDirty()
	// Off the connection it rode on, which it is not running on any
	// more, so closing that connection later leaves this transcript.
	if m := a.machines.stopped(t); m != nil && m.died {
		a.endedAs(t, transportLost)
	}
	// The question first, because the line written into the pane names a
	// way out only when nothing was asked.
	a.askWhatNext(t)
	a.sayTheProgramHasFinished(t)
	// The session rather than the pane: what the program printed stays
	// on screen, and a terminal that was closed could take no new one.
	if err := a.letGoOfTheSession(t); err != nil {
		// The dot has already gone grey: the stream ended, and that is
		// what closed the meter. So the note is the only place left to
		// say the channel was not let go of, and the caller shows the
		// reason.
		e.Note = "could not be closed"
		return err
	}
	return nil
}

// letGoOfTheSession closes the session a pane was reading, leaving the
// pane itself open with what the program printed.
func (a *app) letGoOfTheSession(t *term.Terminal) error {
	s := a.started[t]
	if s == nil || s.on == nil {
		return nil
	}
	// The terminal stops feeding a session it no longer owns, which would
	// otherwise leave a goroutine parked on it for the life of the window.
	t.LetGo()
	return s.on.Close()
}

// sayTheProgramHasFinished writes one line into a pane whose program has
// gone, so the user is not left at a screen that has stopped answering
// with nothing to say why.
//
// Into the pane rather than beside it, so it lands in the transcript,
// and once, because paneEnded marks the pane before it calls this. It is
// the record that the program went; what to do about it is the question
// on the last row.
//
// A pane that asks nothing is named a way out here instead. The question
// is where the other panes say it, and the sidebar that would say it too
// can be hidden.
func (a *app) sayTheProgramHasFinished(t *term.Terminal) {
	if t.Asking() != "" {
		t.Say("-- gridterm: the program has finished --")
		return
	}
	how := "Close this pane when you have read it."
	if chord := a.chordFor("pane.close"); chord != "" {
		how = chord + " closes this pane."
	}
	t.Say("-- gridterm: the program has finished. " + how + " --")
}

// Ended reports whether a pane is one that has stopped, so it is being
// read rather than typed into.
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

// focusPane moves focus n panes along the tree, wrapping at the ends.
func (a *app) focusPane(n int) error {
	var panes []ui.Widget
	for _, leaf := range ui.Leaves(a.root.Widget()) {
		if a.isPane(leaf) {
			panes = append(panes, leaf)
		}
	}
	stepFocus(panes, ui.FocusedLeaf(a.root.Widget()), n, a.focus)
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

// reapExited deals with the panes whose program has gone, from the
// drawing goroutine where the tree may be touched.
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
// pane, rather than going on the stage.
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
// A split that cannot be made falls back to the stage, whatever
// the reason: the pane it was to sit beside may have closed, or gone
// into the background, or the window may have been made too narrow while
// the connection was on its way. The terminal is open either way, and
// losing the login the user waited for because the room for it went
// would be worse than putting it somewhere else.
func (a *app) place(next ui.Widget, at *spot) error {
	if at == nil {
		return a.placePane(next)
	}
	if err := a.canSplit(at.dir, at.beside, next); err != nil {
		return a.placePane(next)
	}
	return a.splitWith(at.dir, at.beside, next)
}

// splitWith divides one pane and puts another in the half that opens up.
//
// next may be a pane that is already somewhere else in the tree, which
// is what splitting with an existing pane means: it is taken out of where
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
// puts it on the stage.
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
