package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/settings"
)

// commandDialog asks for a command to run in a pane of its own, on the
// focused pane's machine.
func (w *window) commandDialog(u *gunim.UI) {
	machine := w.machineOf(w.focused)
	for _, rw := range w.remoteWindows {
		if rw.Name == machine {
			w.toasts.Show(widget.Toast{Title: machine + " is a gunimterm window", Body: "It has no shell to run a command in. Open a terminal on it instead."}, u)
			return
		}
	}
	where := machine
	if where == "" {
		where = "this computer"
	}
	line, dir := widget.NewTextField(), widget.NewTextField()
	line.Placeholder, dir.Placeholder = "such as top, or make test", "optional: where the login lands"
	keep := widget.NewCheckbox("Save this command")
	form := widget.NewForm().Add("Command", line).Add("Folder", dir)
	var kept []settings.SavedCommand
	for _, c := range w.savedCommands {
		if w.savedCommandOn(c) == machine {
			kept = append(kept, c)
		}
	}
	if len(kept) > 0 {
		names := []string{"A new one"}
		for _, c := range kept {
			names = append(names, c.Line)
		}
		pick := widget.NewDropdown(names...)
		pick.Label = "Saved"
		pick.OnPick(func(i int, u *gunim.UI) {
			if i == 0 || i > len(kept) {
				return
			}
			line.SetText(kept[i-1].Line)
			dir.SetText(kept[i-1].Dir)
			keep.SetOn(true, u)
			u.Invalidate()
		})
		form.Add("Saved", pick)
	}
	form.Add("", keep)
	d := widget.NewDialog("Run Command on " + where)
	d.Body = form
	d.SetButtons("Run", "Cancel")
	d.Check = func() string {
		if strings.TrimSpace(line.Text()) == "" {
			return "Type a command to run."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent {
		return RunCommand{Machine: machine, Line: line.Text(), Dir: dir.Text(), Keep: keep.On}
	}
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// savedCommandOn is the machine a saved command runs on, by that
// machine's name now.
func (w *window) savedCommandOn(c settings.SavedCommand) string {
	for _, h := range w.saved {
		if c.HostID != "" && h.ID == c.HostID {
			return h.Name
		}
	}
	return c.Host
}

// savedCommandItems are the palette's lines for the saved commands.
func (w *window) savedCommandItems() []widget.PaletteItem {
	var out []widget.PaletteItem
	for _, c := range w.savedCommands {
		where := w.savedCommandOn(c)
		if where == "" {
			where = "this computer"
		}
		out = append(out, widget.PaletteItem{Title: "Run " + c.Line + " on " + where, Also: []string{c.Dir}})
	}
	return out
}

// setSavedCommands takes the saved commands, and puts them in the
// palette when they changed.
func (w *window) setSavedCommands(saved []settings.SavedCommand) {
	if slices.Equal(saved, w.savedCommands) {
		return
	}
	w.savedCommands = saved
	w.servers(w.saved)
}

// runSavedCommand runs saved command at, as the palette lists them.
func (w *window) runSavedCommand(at string, u *gunim.UI) {
	if i, err := strconv.Atoi(at); err == nil && i >= 0 && i < len(w.savedCommands) {
		u.Send(w, RunSavedCommand{Saved: w.savedCommands[i]})
	}
}
