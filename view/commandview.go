package view

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
func (w *Window) commandDialog(u *gunim.UI) { w.commandDialogOn(w.machineOf(w.focused), u) }

// commandDialogOn asks for a command to run on machine.
func (w *Window) commandDialogOn(machine machines.ID, u *gunim.UI) {
	w.commandDialogAt(machine, app.Placement{}, u)
}

// commandDialogAt asks for a command to run on machine, its pane put
// where at says.
func (w *Window) commandDialogAt(machine machines.ID, at app.Placement, u *gunim.UI) {
	where := w.nameOf(machine)
	line, dir := widget.NewTextField(), widget.NewTextField()
	line.Placeholder, dir.Placeholder = "such as top, or make test", "optional: where the login lands"
	keep := widget.NewCheckbox("Save this command")
	form := widget.NewForm().Add("Command", line).Add("Folder", dir)
	// Every saved command, newest first: this machine's, then the rest,
	// named with where they were saved, to run here too.
	var kept []settings.SavedCommand
	here := 0
	for _, c := range w.savedCommands {
		if w.keptFor(c.Host, c.HostID, machine) {
			kept = slices.Insert(kept, here, c)
			here++
		} else {
			kept = append(kept, c)
		}
	}
	// picked is the saved command of this machine picked, which
	// unticking Keep forgets; filled the folder it put in the Folder
	// field, which a pick after may replace.
	picked, filled := "", ""
	if len(kept) > 0 {
		names := []string{"A new one"}
		for i, c := range kept {
			if i < here {
				names = append(names, c.Line)
				continue
			}
			on := w.savedCommandOn(c)
			if on == "" {
				on = "this computer"
			}
			names = append(names, c.Line+" (on "+on+")")
		}
		pick := widget.NewDropdown(names...)
		pick.Label = "Saved"
		pick.OnPick(func(i int, u *gunim.UI) {
			if i == 0 || i > len(kept) {
				return
			}
			c := kept[i-1]
			line.SetText(c.Line)
			// The folder only when this machine's, and only over one
			// typed by nobody: a folder elsewhere is nothing here.
			mine := i-1 < here
			switch {
			case mine && (dir.Text() == "" || dir.Text() == filled):
				dir.SetText(c.Dir)
				filled = c.Dir
			case !mine && dir.Text() == filled:
				// A folder a pick here put in is nothing to a command
				// saved elsewhere.
				dir.SetText("")
				filled = ""
			}
			picked = ""
			if mine {
				picked = c.Line
			}
			keep.SetOn(mine, u)
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
		if !keep.On && picked != "" && strings.Join(strings.Fields(line.Text()), " ") == picked {
			forget = picked
		}
		return app.RunCommand{Machine: machine, Line: line.Text(), Dir: dir.Text(), Keep: keep.On, Forget: forget, Beside: at.Beside, Vertical: at.Vertical, Instead: at.Instead}
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// savedCommandOn is the machine a saved command runs on, by that
// machine's name now: beyond a window, the window's name for it through
// that window.
func (w *Window) savedCommandOn(c settings.SavedCommand) string {
	host, far, beyond := strings.Cut(c.Host, app.KeptFarSep)
	on := host
	for _, h := range w.saved {
		if c.HostID != "" && h.ID == c.HostID {
			on = h.Name
		}
	}
	if beyond {
		return far + " through " + on
	}
	return on
}

// keptFor reports whether something kept for next time, on the saved
// server hostID, or on host as it was kept with none, is for machine:
// the saved server by its ID, this computer, or a quick connection by
// its address. Beyond a window, it is that window, kept so, and the
// window's name for the machine.
func (w *Window) keptFor(host, hostID string, machine machines.ID) bool {
	keptWindow, keptName, keptFar := strings.Cut(host, app.KeptFarSep)
	window, _, far := machine.Far()
	switch {
	case keptFar != far:
		return false
	case far:
		return w.farHostName(machine) == keptName && w.keptFor(keptWindow, hostID, window)
	}
	switch {
	case hostID != "":
		return machines.ID(hostID) == machine
	case host == "":
		return machine == machines.Local
	}
	return machine != "" && w.nameOf(machine) == host
}

// savedCommandItems are the palette's lines for the saved commands.
func (w *Window) savedCommandItems() []widget.PaletteItem {
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
func (w *Window) setSavedCommands(saved []settings.SavedCommand) {
	if slices.Equal(saved, w.savedCommands) {
		return
	}
	w.savedCommands = saved
	w.servers(w.saved)
}

// termProgramDialog asks what new shells here are told the terminal is
// called.
func (w *Window) termProgramDialog(u *gunim.UI) {
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
