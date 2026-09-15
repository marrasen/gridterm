package main

import (
	"errors"
	"fmt"
	"image/color"

	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/ui/term"
)

// splitFocused asks what goes in the half that opens up, and divides the
// focused pane with whatever is picked.
//
// A split used to be a new local shell and nothing else. What the user
// wants beside a pane is usually something they already have open, or
// the same machine again, and neither of those is a local shell.
func (a *app) splitFocused(dir ui.Dir) error {
	current := a.paneToPlaceBeside()
	// Asked before the question rather than after the answer: there is
	// no point choosing what to put somewhere it could never go.
	if err := a.canDivide(dir, current); err != nil {
		return err
	}

	var hide func()
	c := ui.NewChooser("Split with…", func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	a.addSplitChoices(c, dir, current)
	hide = a.showModal(c, nil)
	if a.root.Modal() != ui.Widget(c) {
		return errors.New("there is no room to ask")
	}
	a.markDirty()
	return nil
}

// addSplitChoices puts everything that could go beside a pane on the
// chooser: a shell here, whatever else is already open, and every
// machine the window knows about.
func (a *app) addSplitChoices(c *ui.Chooser, dir ui.Dir, current ui.Widget) {
	// First, and so the line the chooser opens on: Enter straight after
	// the split key is the shell a split used to give without asking.
	c.Add("New terminal", groupName(a.localHost), func() error {
		return a.splitNewTerminal(dir, current)
	})

	// What is already open, so a pane can be moved in beside this one
	// rather than a second one being started.
	for _, w := range a.otherPanes(current) {
		pane := w
		c.Add("Move "+a.paneName(pane), a.paneWhere(pane), func() error {
			// Asked again now rather than when the line was written: a
			// shell that ended or a connection that dropped while the
			// question was up takes its pane with it, and splicing a
			// closed one back into the tree leaves a dead session where
			// nothing can reach it.
			if !a.live(pane) || !a.live(current) {
				return errors.New("that pane has closed")
			}
			if err := a.splitWith(dir, current, pane); err != nil {
				// It is out of the tree now and nothing else holds it,
				// so it goes back as a tab rather than being left
				// running where nobody can see it.
				if a.stage != nil && ui.ParentOf(a.root.Widget(), pane) == nil {
					a.stage.Add(pane)
					a.relayout()
					a.focus(pane)
				}
				return err
			}
			return nil
		})
	}

	// And every machine, so the half that opens up can be a terminal on
	// one of them. A machine nothing is connected to yet is connected to
	// first, and the terminal lands in the split when it arrives.
	for _, host := range a.allHosts() {
		on := a.about(host)
		if on.kind == hostHere {
			// A terminal there is the local shell already offered.
			continue
		}
		name := on.name
		at := &spot{beside: current, dir: dir}
		c.Add("Terminal on "+name, hostNote(on), func() error {
			// Through the one place that says what a name is worth
			// opening on, so a window here is taken over rather than
			// logged in to, and lands in the split all the same.
			return a.openTerminalOn(name, at)
		})
	}
}

// splitNewTerminal divides a pane with a shell on this machine.
func (a *app) splitNewTerminal(dir ui.Dir, current ui.Widget) error {
	next, err := a.newTerminal()
	if err != nil {
		return err
	}
	if err := a.splitWith(dir, current, next); err != nil {
		// Nowhere in the tree, so nothing else knows about it. Closing
		// the terminal closes its shell with it.
		delete(a.panes, next)
		return errors.Join(err, next.Close())
	}
	a.showPane(next)
	return nil
}

// splitHere divides the focused pane with a shell on this machine,
// without asking. It is what the first line of the chooser does, for a
// caller that already knows the answer.
func (a *app) splitHere(dir ui.Dir) error {
	current := a.paneToPlaceBeside()
	if err := a.canDivide(dir, current); err != nil {
		return err
	}
	return a.splitNewTerminal(dir, current)
}

// otherPanes is everything open in the window that could be moved
// somewhere else, except one pane, in the order the stage holds them.
//
// A file pane is left out: it belongs to the file manager, and out of it
// it loses every key it has.
func (a *app) otherPanes(except ui.Widget) []ui.Widget {
	if a.stage == nil {
		return nil
	}
	var out []ui.Widget
	for _, leaf := range ui.Leaves(a.stage) {
		if leaf == except || !a.isPane(leaf) {
			continue
		}
		if _, ok := leaf.(*files.Pane); ok {
			continue
		}
		out = append(out, leaf)
	}
	return out
}

// paneName is what the sidebar calls a pane, for a list that has to say
// which one it means.
func (a *app) paneName(w ui.Widget) string {
	// Named rather than drawn: a chooser is a list of things to pick
	// from, and the picture the sidebar uses says which kind but not
	// which one.
	switch pane := w.(type) {
	case *term.Terminal:
		if e := a.panes[pane]; e != nil {
			return e.Kind.String() + " " + e.Label
		}
	case *files.Pane:
		if a.files != nil {
			if e := a.files.rows[pane]; e != nil {
				return e.Kind.String() + " " + e.Label
			}
		}
	}
	return fmt.Sprintf("%T", w)
}

// paneWhere is the machine a pane is on, for the note at the end of its
// line.
func (a *app) paneWhere(w ui.Widget) string {
	switch pane := w.(type) {
	case *term.Terminal:
		if m := a.paneOn[pane]; m != nil {
			return m.at.name
		}
		if e := a.panes[pane]; e != nil {
			return groupName(e.Host)
		}
	case *files.Pane:
		return groupName(a.hostOf(pane.FS()))
	}
	return ""
}

// hostNote says what picking a machine costs, so the user can tell which
// choices are a login and which are already there.
func hostNote(on hostFacts) string {
	switch {
	case on.kind == hostWindow:
		return "already taken over"
	case on.kind == hostMachine:
		return "connected"
	case on.kind == hostConnecting:
		return "still connecting"
	case on.serves:
		return "takes it over"
	}
	return "connects"
}

// chooserStyle colours a chooser in the window's own colours.
func (a *app) chooserStyle() ui.ChooserStyle {
	return ui.ChooserStyle{
		FG: a.colours.FG,
		// No background of its own: the frosted panel behind it is the
		// background, and an opaque fill would hide it.
		BG:         color.RGBA{},
		TitleFG:    a.colours.FG,
		SelectedFG: a.colours.BG,
		SelectedBG: a.colours.FG,
		// Dimmer than the line: where something is, is a note beside it
		// rather than part of its name.
		NoteFG:   a.colours.ANSI[8],
		BorderFG: a.colours.ANSI[8],
		ShadowBG: shadow,
	}
}
