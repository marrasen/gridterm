package main

import (
	"strings"

	"github.com/marrasen/gunim"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
)

// The window's shortcuts: gridterm's own chords, for the commands this
// window has so far. Each is Ctrl+Shift and a key, or Ctrl with a key
// no shell reads, so the shell keeps the rest.

// shortcuts returns the chords the window takes, by command.
func shortcuts() *ui.Keymap {
	keys := ui.NewKeymap()
	keys.MustBind(map[ui.Chord]string{
		{Key: input.KeyD, Mods: input.ModCtrl | input.ModShift}:   "pane.splitRight",
		{Key: input.KeyE, Mods: input.ModCtrl | input.ModShift}:   "pane.splitDown",
		{Key: input.KeyU, Mods: input.ModCtrl | input.ModShift}:   "pane.popOut",
		{Key: input.KeyW, Mods: input.ModCtrl | input.ModShift}:   "pane.close",
		{Key: input.KeyTab, Mods: input.ModCtrl}:                  "pane.next",
		{Key: input.KeyTab, Mods: input.ModCtrl | input.ModShift}: "pane.previous",
		{Key: input.KeyPageDown, Mods: input.ModCtrl}:             "pane.nextInSidebar",
		{Key: input.KeyPageUp, Mods: input.ModCtrl}:               "pane.previousInSidebar",
		{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}:   "conn.terminal",
		{Key: input.KeyB, Mods: input.ModCtrl | input.ModShift}:   "sidebar.toggle",
		{Key: input.KeyV, Mods: input.ModCtrl | input.ModShift}:   "edit.paste",
		{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}:   "edit.copy",
		{Key: input.KeyInsert, Mods: input.ModCtrl}:               "edit.copy",
		{Key: input.KeyInsert, Mods: input.ModShift}:              "edit.paste",
		{Key: input.KeyPageUp, Mods: input.ModShift}:              "view.scrollUp",
		{Key: input.KeyPageDown, Mods: input.ModShift}:            "view.scrollDown",
		{Key: input.KeyK, Mods: input.ModCtrl | input.ModShift}:   "palette.open",
		{Key: input.KeyF10}: "menu.open",
		{Key: input.KeyF11}: "view.fullScreen",
		{Key: input.KeyL, Mods: input.ModCtrl | input.ModShift}:      "sidebar.focus",
		{Key: input.KeyH, Mods: input.ModCtrl | input.ModShift}:      "help.shortcuts",
		{Key: input.KeyPlus, Mods: input.ModCtrl}:                    "font.increase",
		{Key: input.KeyG, Mods: input.ModCtrl | input.ModShift}:      "files.goTo",
		{Key: input.KeyN, Mods: input.ModCtrl | input.ModShift}:      "server.connect",
		{Key: input.KeyEquals, Mods: input.ModCtrl}:                  "font.increase",
		{Key: input.KeyEquals, Mods: input.ModCtrl | input.ModShift}: "font.increase",
		{Key: input.KeyMinus, Mods: input.ModCtrl}:                   "font.decrease",
		{Key: input.Key0, Mods: input.ModCtrl}:                       "font.reset",
		{Key: input.KeyA, Mods: input.ModCtrl | input.ModShift}:      "pane.switch",
	})
	return keys
}

// commands are what the palette offers, in gridterm's words.
var commands = []struct{ id, title string }{
	{"conn.terminal", "New Terminal"},
	{"pane.splitRight", "Split Right"},
	{"pane.splitDown", "Split Down"},
	{"pane.popOut", "Pop Out Pane"},
	{"pane.close", "Close Pane"},
	{"pane.next", "Next Pane"},
	{"pane.previous", "Previous Pane"},
	{"pane.switch", "Switch Pane"},
	{"pane.rename", "Rename Pane"},
	{"sidebar.toggle", "Show or Hide Sidebar"},
	{"theme.pick", "Pick a Theme"},
	{"font.increase", "Larger Font"},
	{"font.decrease", "Smaller Font"},
	{"font.reset", "Reset Font Size"},
	{"server.connect", "Connect to Server"},
	{"conn.files", "Files Here"},
	{"files.goTo", "Go to Directory"},
	{"secrets.show", "Show Secrets"},
	{"secrets.add", "Add Secret"},
	{"secrets.addNote", "Add Note"},
	{"secrets.lock", "Lock Secrets"},
	{"secrets.export", "Export Secrets"},
	{"secrets.import", "Import Secrets"},
	{"agent.share", "Share with an Agent"},
	{"agent.permissions", "Agent Permissions"},
	{"serve.window", "Serve This Window"},
	{"serve.attach", "Connect to Window"},
	{"conn.disconnect", "Disconnect"},
	{"pane.titles", "Show Pane Titles"},
	{"view.fullScreen", "Full Screen"},
	{"conn.command", "Run Command"},
	{"pane.scrollback", "Find in Scrollback"},
	{"sidebar.focus", "Focus Sidebar"},
	{"sidebar.closeRow", "Close Selected Row"},
	{"conn.clearFinished", "Clear Finished"},
	{"server.editThis", "Edit This Server"},
	{"server.forget", "Remove This Server"},
	{"server.reload", "Reload Server List"},
	{"shell.setup", "Shell Setup"},
	{"shell.termProgram", "Terminal Identity"},
	{"help.shortcuts", "Shortcuts and Commands"},
	{"shortcuts.write", "New Shortcuts File"},
	{"shortcuts.reload", "Reload Shortcuts"},
	{"view.themesStart", "New Theme File"},
	{"view.themesReload", "Reload Themes"},
	{"help.files", "File Locations"},
	{"app.about", "About gridterm"},
	{"sshkey.make", "New SSH Key"},
	{"sshkey.lock", "Lock SSH Keys"},
	{"view.jobs", "Show Jobs"},
	{"view.log", "Window Log"},
	{"conn.log", "Connection Log"},
	{"tunnel.open", "Open Tunnel"},
	{"tunnel.socks", "Open SOCKS Proxy"},
	{"edit.copy", "Copy"},
	{"edit.paste", "Paste"},
}

// menus are the menubar's menus, in gridterm's order and words, with
// the commands this window has so far. A line goes above an item that
// starts a group.
var menus = []struct {
	title string
	items []menuItem
}{
	{"File", []menuItem{
		{id: "conn.terminal", title: "New Terminal"},
		{title: "Close", caption: true}, {id: "pane.close", title: "Pane"},
		{id: "app.exit", title: "Exit", group: true},
	}},
	{"Edit", []menuItem{
		{id: "edit.copy", title: "Copy"}, {id: "edit.paste", title: "Paste"},
		{id: "pane.scrollback", title: "Find in Scrollback…", group: true},
	}},
	{"View", []menuItem{
		{id: "sidebar.toggle", title: "Sidebar"},
		{id: "pane.titles", title: "Pane Titles"},
		{id: "view.fullScreen", title: "Full Screen"},
		{id: "view.jobs", title: "Jobs"},
		{title: "Font", caption: true},
		{id: "font.increase", title: "Larger"}, {id: "font.decrease", title: "Smaller"}, {id: "font.reset", title: "Reset"},
		{title: "Scrollback", caption: true},
		{id: "view.scrollUp", title: "Page Up"}, {id: "view.scrollDown", title: "Page Down"},
	}},
	{"Pane", []menuItem{
		{title: "Split", caption: true},
		{id: "pane.splitRight", title: "Right"}, {id: "pane.splitDown", title: "Down"}, {id: "pane.popOut", title: "Pop Out"},
		{title: "Go To", caption: true},
		{id: "pane.nextInSidebar", title: "Next"}, {id: "pane.previousInSidebar", title: "Previous"},
		{id: "pane.switch", title: "All Panes…"}, {id: "sidebar.focus", title: "Sidebar"},
		{id: "pane.rename", title: "Rename…", group: true},
		{id: "conn.clearFinished", title: "Clear Finished"},
	}},
	{"Machine", []menuItem{
		{title: "Open Here", caption: true},
		{id: "conn.terminal", title: "Terminal"}, {id: "conn.command", title: "Command…"},
		{id: "conn.files", title: "Files"},
		{id: "tunnel.open", title: "Tunnel…"}, {id: "tunnel.socks", title: "SOCKS Proxy…"},
		{id: "files.goTo", title: "Go to Directory…", group: true},
		{id: "conn.log", title: "Connection Log"},
		{id: "shell.setup", title: "Shell Setup"},
		{id: "conn.disconnect", title: "Disconnect", group: true},
		{id: "server.editThis", title: "Edit This Server…"}, {id: "server.forget", title: "Remove This Server…"},
	}},
	// The Servers menu is made from the saved servers.
	{"Servers", nil},
	{"Share", []menuItem{
		{title: "With an Agent", caption: true},
		{id: "agent.share", title: "Share Panes…"}, {id: "agent.permissions", title: "Permissions…"},
		{title: "With Another Window", caption: true},
		{id: "serve.window", title: "Serve This Window…"}, {id: "serve.attach", title: "Connect to Window…"},
	}},
	{"Secrets", []menuItem{
		{id: "secrets.show", title: "Show Secrets"},
		{id: "secrets.add", title: "Add Secret…"}, {id: "secrets.addNote", title: "Add Note…"},
		{id: "secrets.export", title: "Export…", group: true}, {id: "secrets.import", title: "Import…"},
		{id: "secrets.lock", title: "Lock", group: true},
		{title: "SSH Keys", caption: true},
		{id: "sshkey.make", title: "New SSH Key…"}, {id: "sshkey.lock", title: "Lock SSH Keys"},
	}},
	{"Options", []menuItem{
		{id: "theme.pick", title: "Theme…"},
		{id: "shell.termProgram", title: "Terminal Identity…"},
		{title: "Start a File", caption: true},
		{id: "view.themesStart", title: "Themes"}, {id: "shortcuts.write", title: "Shortcuts"},
		{title: "Read Again", caption: true},
		{id: "view.themesReload", title: "Themes"}, {id: "shortcuts.reload", title: "Shortcuts"},
		{id: "server.reload", title: "Server List"},
		{id: "help.files", title: "File Locations…", group: true},
	}},
	{"Help", []menuItem{
		{id: "palette.open", title: "All Commands…"},
		{id: "help.shortcuts", title: "Shortcuts and Commands"},
		{id: "view.log", title: "Window Log", group: true},
		{id: "app.about", title: "About gridterm", group: true},
	}},
}

// menuItem is one line of a menu: a command, or a caption over the
// group under it. A line goes above an item that starts a group, and
// above every caption but a menu's first line.
type menuItem struct {
	id, title string
	group     bool
	caption   bool
}

// chordLabel writes a chord the way a desktop menu does, as
// Ctrl+Shift+K.
func chordLabel(c ui.Chord) string {
	parts := strings.Split(c.String(), "+")
	for i, s := range parts {
		if s != "" {
			parts[i] = strings.ToUpper(s[:1]) + s[1:]
		}
	}
	return strings.Join(parts, "+")
}

// commandIntent returns what the window asks the program for, for a
// command the program carries out.
func commandIntent(id string) (gunim.Intent, bool) {
	switch id {
	case "pane.splitRight":
		return SplitPane{}, true
	case "pane.splitDown":
		return SplitPane{Vertical: true}, true
	case "pane.popOut":
		return PopOut{}, true
	case "pane.close":
		return ClosePane{}, true
	case "pane.next", "pane.nextInSidebar":
		return NextPane{}, true
	case "pane.previous", "pane.previousInSidebar":
		return NextPane{Back: true}, true
	case "conn.terminal":
		return NewTerminal{}, true
	case "sidebar.toggle":
		return ToggleSidebar{}, true
	case "app.exit":
		return Exit{}, true
	case "conn.files":
		return OpenFiles{}, true
	case "view.jobs":
		return ShowJobs{}, true
	case "server.reload":
		return ReloadServers{}, true
	case "shell.setup":
		return ToggleShellSetup{}, true
	case "sshkey.lock":
		return LockKeys{}, true
	case "shortcuts.reload":
		return ReloadShortcuts{}, true
	case "view.themesReload":
		return ReloadThemes{}, true
	case "view.themesStart":
		return WriteThemeFile{}, true
	case "conn.clearFinished":
		return ClearFinished{}, true
	case "secrets.show":
		return ShowSecrets{}, true
	case "secrets.lock":
		return LockSecrets{}, true
	case "view.log":
		return ShowLog{}, true
	case "font.increase":
		return FontSize{Step: 1}, true
	case "font.decrease":
		return FontSize{Step: -1}, true
	case "font.reset":
		return FontSize{}, true
	}
	return nil, false
}
