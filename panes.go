package main

import (
	"errors"
	"fmt"

	"github.com/marrasen/gridterm/ui"
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
	t, err := term.New(term.Config{
		Session:        sess,
		Size:           ui.Size{Cols: a.lastSize[0], Rows: a.lastSize[1]},
		Scrollback:     a.scrollback,
		Palette:        &a.palette,
		ReadClipboard:  clipboardRead,
		WriteClipboard: a.clip.set,
		OnExit:         a.paneExited,
		OnError:        a.logError,
	})
	if err != nil {
		// The session is ours now and nothing else will close it.
		_ = sess.Close()
		return nil, fmt.Errorf("start terminal: %w", err)
	}
	a.panes[t] = struct{}{}
	return t, nil
}

// newSplit builds a split carrying the window's divider colours.
func (a *app) newSplit(dir ui.Dir, first, second ui.Widget) *ui.Split {
	s := ui.NewSplit(dir, first, second)
	s.DividerFG = a.palette.FG
	s.DividerBG = a.palette.BG
	return s
}

// splitFocused puts a new shell beside the focused pane.
func (a *app) splitFocused(dir ui.Dir) error {
	current := ui.FocusedLeaf(a.root.Widget())
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
	return nil
}

// closeFocused shuts the focused pane and gives its room to whatever
// shared the split. Closing the last pane closes the window.
func (a *app) closeFocused() error {
	return a.closePane(ui.FocusedLeaf(a.root.Widget()))
}

// closePane takes a pane out of the tree and ends every shell under it.
//
// It works on any container, not just a split, because a pane can sit
// inside anything: asking the tree to detach it is what keeps this from
// having to know.
func (a *app) closePane(w ui.Widget) error {
	if w == nil {
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
		// The last pane. The window goes with it.
		a.quit.Store(true)
	default:
		if root != a.root.Widget() {
			a.root.SetWidget(root)
		}
		a.relayout()
		a.focus(ui.FocusedLeaf(a.root.Widget()))
	}

	var err error
	for _, leaf := range doomed {
		t, isTerm := leaf.(*term.Terminal)
		if !isTerm {
			continue
		}
		delete(a.panes, t)
		if cerr := t.Close(); cerr != nil && err == nil {
			err = cerr
		}
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
			if err := a.closePane(t); err != nil {
				a.logError(err)
			}
		}
	}
}
