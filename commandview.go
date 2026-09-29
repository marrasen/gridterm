package main

import (
	"slices"
	"strings"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/settings"
)

// commandDialog asks for a command to run in a pane of its own, on the
// focused pane's machine.
func (w *window) commandDialog(u *gunim.UI) { w.commandDialogOn(w.machineOf(w.focused), u) }

// commandDialogOn asks for a command to run on machine.
func (w *window) commandDialogOn(machine machines.ID, u *gunim.UI) {
	w.commandDialogAt(machine, app.Placement{}, u)
}

// commandDialogAt asks for a command to run on machine, its pane put
// where at says.
func (w *window) commandDialogAt(machine machines.ID, at app.Placement, u *gunim.UI) {
	for _, rw := range w.remoteWindows {
		if rw.Name == machine {
			w.toasts.Show(widget.Toast{Title: w.nameOf(machine) + " is a kakel window", Body: "It has no shell to run a command in. Open a terminal on it instead."}, u)
			return
		}
	}
	where := w.nameOf(machine)
	line, dir := widget.NewTextField(), widget.NewTextField()
	line.Placeholder, dir.Placeholder = "such as top, or make test", "optional: where the login lands"
	keep := widget.NewCheckbox("Save this command")
	form := widget.NewForm().Add("Command", line).Add("Folder", dir)
	var kept []settings.SavedCommand
	// picked is the saved command picked, which unticking Keep forgets.
	picked := ""
	for _, c := range w.savedCommands {
		if w.keptFor(c.Host, c.HostID, machine) {
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
			picked = kept[i-1].Line
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
		forget := ""
		if !keep.On && picked != "" && line.Text() == picked {
			forget = picked
		}
		return app.RunCommand{Machine: machine, Line: line.Text(), Dir: dir.Text(), Keep: keep.On, Forget: forget, Beside: at.Beside, Vertical: at.Vertical}
	}
	d.Dismiss = app.DialogClosed{}
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

// keptFor reports whether something kept for next time, on the saved
// server hostID, or on host as it was kept with none, is for machine:
// the saved server by its ID, this computer, or a quick connection by
// its address.
func (w *window) keptFor(host, hostID string, machine machines.ID) bool {
	switch {
	case hostID != "":
		return machines.ID(hostID) == machine
	case host == "":
		return machine == machines.Local
	}
	return machine != "" && w.nameOf(machine) == host
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

// termProgramDialog asks what new shells here are told the terminal is
// called.
func (w *window) termProgramDialog(u *gunim.UI) {
	called := widget.NewTextField()
	called.SetText(w.termProgram)
	called.Placeholder = "kakel"
	known := widget.NewDropdown(append([]string{"kakel"}, app.KnownTerminals...)...)
	known.Label = "Known terminals"
	known.OnPick(func(i int, u *gunim.UI) {
		if i == 0 {
			called.SetText("")
		} else {
			called.SetText(app.KnownTerminals[i-1])
		}
		u.Invalidate()
	})
	d := widget.NewDialog("Terminal Identity")
	d.Body = widget.NewForm().
		Add("", widget.NewLabel("Programs read TERM_PROGRAM to identify the terminal. Blank reports kakel. Another name can turn on features such as pictures, and can also bring sequences that show as text. It applies to new panes.")).
		Add("TERM_PROGRAM", called).Add("Known", known)
	d.SetButtons("Save", "Cancel")
	d.OnAccept = func() gunim.Intent { return app.SetTermProgram{Called: called.Text()} }
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}
