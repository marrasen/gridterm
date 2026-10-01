package view

import (
	"slices"
	"strings"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/words"

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
		Add("", widget.NewLabel("Programs read TERM_PROGRAM to identify the terminal. Blank reports kakel. Another name can turn on features such as images, and can also bring sequences that show as text. It applies to new panes.")).
		Add("TERM_PROGRAM", called).Add("Known", known)
	d.SetButtons("Save", "Cancel")
	d.OnAccept = func() gunim.Intent { return app.SetTermProgram{Called: called.Text()} }
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// launcherKeyDialog asks for the launcher's key.
func (w *Window) launcherKeyDialog(u *gunim.UI) {
	key := widget.NewTextField()
	key.SetText(w.launcherKey)
	key.Placeholder = app.DefaultLauncherKey
	d := widget.NewDialog("Launcher Key")
	d.Body = widget.NewForm().
		Add("", widget.NewLabel("Opens the launcher from any program. Super is the Windows key; none takes no key.")).
		Add("Key", key)
	d.SetButtons("Save", "Cancel")
	d.Check = func() string {
		k := strings.TrimSpace(key.Text())
		if k == "" || strings.EqualFold(k, "none") {
			return ""
		}
		if _, err := app.LauncherHotKey(k); err != nil {
			return words.UpperFirst(err.Error()) + "."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent { return app.SetLauncherKey{Key: key.Text()} }
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// installDialog asks how to install this copy of kakel.
func (w *Window) installDialog(u *gunim.UI) {
	desk := widget.NewCheckbox("Shortcut on the desktop")
	start := widget.NewCheckbox("Start with the computer, in the tray")
	auto := widget.NewCheckbox("Update automatically")
	d := widget.NewDialog("Install kakel")
	d.Body = widget.NewForm().
		Add("", widget.NewLabel("For you alone, with a Start menu entry. No administrator needed.")).
		Add("", desk).Add("", start).Add("", auto)
	d.SetButtons("Install", "Cancel")
	d.OnAccept = func() gunim.Intent {
		return app.InstallKakel{Desktop: desk.On, Autostart: start.On, AutoUpdate: auto.On}
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// updateChoices are the update setting's choices, as the dialog lists
// them and the settings write them.
var updateChoices = []struct{ title, value string }{
	{"Tell me", settings.UpdatesNotify}, {"Install them", settings.UpdatesInstall}, {"Off", settings.UpdatesOff},
}

// updatesDialog asks what kakel does with a newer release.
func (w *Window) updatesDialog(u *gunim.UI) {
	var titles []string
	at := 0
	for i, c := range updateChoices {
		titles = append(titles, c.title)
		if c.value == w.update.Updates {
			at = i
		}
	}
	pick := widget.NewDropdown(titles...)
	pick.Selected = at
	d := widget.NewDialog("Updates")
	d.Body = widget.NewForm().
		Add("New releases", pick).
		Add("", widget.NewLabel("Installed, an update starts the next time kakel does."))
	d.SetButtons("Save", "Cancel")
	d.AddAction("Check Now", func(u *gunim.UI) { d.Close(u); u.Send(w, app.CheckUpdates{}) })
	d.OnAccept = func() gunim.Intent {
		return app.SetUpdates{What: updateChoices[max(0, min(pick.Selected, len(updateChoices)-1))].value}
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// thisComputerDialog edits this computer's settings, as Edit This
// Server… does a server's: the folder a new terminal starts in, and the
// shell it runs.
func (w *Window) thisComputerDialog(u *gunim.UI) {
	start := widget.NewTextField()
	start.Placeholder = "your home folder"
	start.SetText(w.thisComputer.StartFolder)
	titles, ids := []string{"Your default shell"}, []string{""}
	for _, sh := range w.shellChoices {
		titles = append(titles, sh.Title)
		ids = append(ids, sh.ID)
	}
	shell := widget.NewDropdown(titles...)
	shell.Label = "Shell"
	if i := slices.Index(ids, w.chosenShell); i > 0 {
		shell.Selected = i
	}
	d := widget.NewDialog("This Computer")
	d.Body = widget.NewForm().
		Add("Start in", start).
		Add("", widget.NewLabel("New terminals start here, unless you started kakel in a folder of your own, or open them from a terminal that is in another folder.")).
		Add("Shell", shell)
	d.SetButtons("Save", "Cancel")
	d.Check = func() string {
		if s := strings.TrimSpace(start.Text()); s != "" {
			if _, err := app.FolderHere(s); err != nil {
				return words.UpperFirst(err.Error()) + "."
			}
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent {
		return app.SaveThisComputer{
			StartFolder: strings.TrimSpace(start.Text()),
			Shell:       ids[max(0, min(shell.Selected, len(ids)-1))],
		}
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}
