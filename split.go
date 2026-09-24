package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/marrasen/gridterm/conns"
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
	c := ui.NewChooser("Split with", func() {
		if hide != nil {
			hide()
		}
	})
	c.Style = a.chooserStyle()
	a.addSplitChoices(c, dir, current)
	hide = a.showModal(c, nil)
	if a.root.Modal() != ui.Widget(c) {
		return errors.New("Window too small")
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
	c.Add("New terminal", groupName(a.newPaneHost()), func() error {
		return a.splitNewTerminal(dir, current)
	})

	if a.newPaneHost() == conns.Local {
		a.addShellChoices(c, dir, current)
	}

	// What is already open, so a pane can be moved in beside this one
	// rather than a second one being started. Under the machine each is
	// on, the way the sidebar groups them.
	for _, w := range a.panesToMove(current) {
		pane := w
		c.Under(a.paneWhere(pane))
		c.Add("Move "+a.paneName(pane), "", func() error {
			// Asked again now rather than when the line was written: a
			// pane closed while the question was up has gone, and
			// splicing a closed one back into the tree leaves a dead
			// session where nothing can reach it.
			if !a.live(pane) || !a.live(current) {
				return errors.New("that pane has closed")
			}
			if err := a.splitWith(dir, current, pane); err != nil {
				// It is out of the tree now and nothing else holds it,
				// so it goes back on the stage rather than being left
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
	c.Under("")

	// And every machine, so the half that opens up can be a terminal on
	// one of them. A machine nothing is connected to yet is connected to
	// first, and the terminal lands in the split when it arrives.
	for _, host := range a.allHosts() {
		on := a.about(host)
		name := on.name
		at := &spot{beside: current, dir: dir}
		// A terminal where new panes go is the line offered first.
		if name != a.newPaneHost() {
			// groupName, so the line for this machine reads as a name
			// rather than as the empty string the panel keys it by.
			c.Add("Terminal on "+groupName(name), hostNote(on), func() error {
				// Through the one place that says what a name is worth
				// opening on, so a window here is taken over rather than
				// logged in to, and lands in the split all the same.
				return a.openTerminalOn(name, at)
			})
			if name == conns.Local {
				a.addShellChoices(c, dir, current)
			}
		}
		if on.runsCommands() {
			c.Add("Command on "+groupName(name)+"…", hostNote(on), func() error {
				a.askCommandOn(name, at)
				return nil
			})
		}
	}
}

// panesToMove is every pane a split could take, grouped by the machine
// it is on so that the headings come out in one run each.
func (a *app) panesToMove(except ui.Widget) []ui.Widget {
	out := a.otherPanes(except)
	slices.SortStableFunc(out, func(x, y ui.Widget) int {
		return cmp.Compare(a.paneWhere(x), a.paneWhere(y))
	})
	return out
}

// addShellChoices puts a line per shell this machine has under the line
// that opens a terminal on it, the way the plus on its row does.
func (a *app) addShellChoices(c *ui.Chooser, dir ui.Dir, current ui.Widget) {
	for _, sh := range a.shellPick.list() {
		c.Add(sh.Title, groupName(conns.Local), func() error {
			return a.splitOnShell(dir, current, sh)
		})
	}
}

// splitNewTerminal divides a pane with a new shell, wherever new panes
// open.
func (a *app) splitNewTerminal(dir ui.Dir, current ui.Widget) error {
	return a.splitWithNew(dir, current, a.newTerminal)
}

// splitNewTerminalHere divides a pane with a shell on this machine, for
// the line that names this machine rather than the default one.
func (a *app) splitNewTerminalHere(dir ui.Dir, current ui.Widget) error {
	return a.splitWithNew(dir, current, a.localTerminal)
}

// splitWithNew divides a pane with a shell from start.
func (a *app) splitWithNew(dir ui.Dir, current ui.Widget,
	start func() (*term.Terminal, error)) error {

	next, err := start()
	if err != nil {
		return err
	}
	if err := a.splitWith(dir, current, next); err != nil {
		// Nowhere in the tree, so nothing else knows about it. Closing
		// the terminal closes its shell with it.
		delete(a.panes, next)
		delete(a.started, next)
		return errors.Join(err, next.Close())
	}
	a.showPane(next)
	return nil
}

// splitHere divides the focused pane with a new shell, wherever new
// panes open, without asking. It is what the first line of the chooser
// does, for a caller that already knows the answer.
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
	case *files.Reader:
		if held := a.readers[pane]; held != nil {
			// A reader, whether or not it is following: the chooser says
			// which pane, and following is something the pane is doing.
			return conns.Reader.String() + " " + held.row.Label
		}
	case *jobPane:
		if pane.entry != nil {
			return pane.entry.Kind.String() + " " + pane.entry.Label
		}
	case *secretsPane:
		return conns.Secrets.String()
	case *servingPane:
		return dlgServingWindow
	}
	return fmt.Sprintf("%T", w)
}

// paneWhere is the machine a pane is on, for the note at the end of its
// line.
func (a *app) paneWhere(w ui.Widget) string {
	switch pane := w.(type) {
	case *term.Terminal:
		if m := a.machines.runningOn(pane); m != nil {
			return m.at.name
		}
		if e := a.panes[pane]; e != nil {
			return groupName(e.Host)
		}
	case *files.Pane:
		if _, over := farFS(pane.FS()); over {
			// The machine it reads, through the window: two panes on two
			// machines over one window are both filed under the window, so
			// the window's name alone would read the same on both lines.
			return pane.FS().Name()
		}
		return groupName(a.hostOf(pane.FS()))
	case *files.Reader:
		if held := a.readers[pane]; held != nil {
			return groupName(held.row.Host)
		}
	case *jobPane:
		if pane.entry != nil {
			// The end the work is filed under, which is where its row
			// is on the sidebar.
			return groupName(pane.entry.Host)
		}
	case *secretsPane:
		// The vault is a file beside the window's own settings, so it
		// is on this machine whatever the panes around it are on.
		return groupName(conns.Local)
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
		FG: a.frameFG(),
		// No background of its own unless the theme asked for a flat
		// one: the frosted panel behind it is the background, and an
		// opaque fill would hide it.
		BG:         a.panelBG(),
		TitleFG:    a.frameFG(),
		SelectedFG: a.activeFG(),
		SelectedBG: a.activeBG(),
		// Dimmer than the line: where something is, is a note beside it
		// rather than part of its name.
		NoteFG: a.panelDimFG(),
		// The buttons along the bottom, in the colours a form and a
		// notice draw theirs: the same button, wherever it is.
		ButtonFG:       a.buttonFG(),
		ButtonBG:       a.buttonBG(),
		ActiveFG:       a.activeFG(),
		ActiveBG:       a.activeBG(),
		ButtonShadowBG: a.buttonShadowBG(),
		BorderFG:       a.panelBorderFG(),
		ShadowBG:       a.panelShadow(),
		Rule:           a.panelRule(),
	}
}
