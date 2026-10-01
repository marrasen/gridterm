package view

import (
	"strings"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/icon"

	"github.com/marrasen/kakel/input"
	shellfind "github.com/marrasen/kakel/shells"
	"github.com/marrasen/kakel/ui"
)

// The window's shortcuts: kakel's own chords, for the commands this
// window has so far. Each is Ctrl+Shift and a key, or Ctrl with a key
// no shell reads, so the shell keeps the rest.

// Shortcuts returns the chords the window takes, by command.
func Shortcuts() *ui.Keymap {
	keys := ui.NewKeymap()
	keys.MustBind(map[ui.Chord]string{
		{Key: input.KeyD, Mods: input.ModCtrl | input.ModShift}:        "pane.splitRight",
		{Key: input.KeyE, Mods: input.ModCtrl | input.ModShift}:        "pane.splitDown",
		{Key: input.KeyU, Mods: input.ModCtrl | input.ModShift}:        "pane.popOut",
		{Key: input.KeyW, Mods: input.ModCtrl | input.ModShift}:        "pane.close",
		{Key: input.KeyTab, Mods: input.ModCtrl}:                       "pane.next",
		{Key: input.KeyTab, Mods: input.ModCtrl | input.ModShift}:      "pane.previous",
		{Key: input.KeyPageDown, Mods: input.ModCtrl}:                  "tab.next",
		{Key: input.KeyPageUp, Mods: input.ModCtrl}:                    "tab.previous",
		{Key: input.KeyPageDown, Mods: input.ModCtrl | input.ModShift}: "tab.moveRight",
		{Key: input.KeyPageUp, Mods: input.ModCtrl | input.ModShift}:   "tab.moveLeft",
		{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}:        "conn.terminal",
		{Key: input.KeyB, Mods: input.ModCtrl | input.ModShift}:        "sidebar.toggle",
		{Key: input.KeyV, Mods: input.ModCtrl | input.ModShift}:        "edit.paste",
		{Key: input.KeyV, Mods: input.ModCtrl | input.ModAlt}:          "edit.pasteImage",
		{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}:        "edit.copy",
		{Key: input.KeyInsert, Mods: input.ModCtrl}:                    "edit.copy",
		{Key: input.KeyInsert, Mods: input.ModShift}:                   "edit.paste",
		{Key: input.KeyPageUp, Mods: input.ModShift}:                   "view.scrollUp",
		{Key: input.KeyPageDown, Mods: input.ModShift}:                 "view.scrollDown",
		{Key: input.KeyK, Mods: input.ModCtrl | input.ModShift}:        "palette.open",
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
		{Key: input.KeyA, Mods: input.ModCtrl | input.ModShift}:      "view.switcher",
	})
	return keys
}

// commands are what the palette offers, in gridterm's words.
var commands = []struct{ id, title string }{
	{"conn.terminal", "New Terminal"},
	{"shell.default", "New Terminal, Default Shell"},
	{"pane.splitRight", "Split Right"},
	{"pane.splitDown", "Split Down"},
	{"pane.popOut", "Pop Out Pane"},
	{"pane.close", "Close Pane"},
	{"pane.next", "Next Recent Pane"},
	{"pane.previous", "Previous Recent Pane"},
	{"pane.nextInSidebar", "Next Pane"},
	{"pane.previousInSidebar", "Previous Pane"},
	{"tab.next", "Next Tab"},
	{"tab.previous", "Previous Tab"},
	{"tab.moveRight", "Move Tab Right"},
	{"tab.moveLeft", "Move Tab Left"},
	{"tab.close", "Close Tab"},
	{"tab.newWindow", "Move Tab to New Window"},
	{"servers.window", "Open Servers Window"},
	{"secrets.window", "Open Secrets Window"},
	{"view.switcher", "All Panes"},
	{"pane.rename", "Rename Pane"},
	{"sidebar.toggle", "Servers"},
	{"view.theme", "Choose Theme"},
	{"font.increase", "Larger Font"},
	{"font.decrease", "Smaller Font"},
	{"font.reset", "Reset Font Size"},
	{"server.connect", "Quick Connect"},
	{"server.add", "Add Server"},
	{"server.import", "Import from SSH Config"},
	{"conn.files", "Files Here"},
	{"files.goTo", "Go to Directory"},
	{"files.icons", "Icon View"},
	{"secrets.open", "Show Secrets"},
	{"secrets.pane", "Manage Secrets"},
	{"secrets.change", "Change Secret"},
	{"secrets.forget", "Remove Secret"},
	{"secrets.addKey", "Add Secrets Key"},
	{"secrets.addPassphrase", "Add Secrets Passphrase"},
	{"secrets.removeKey", "Remove Secrets Key"},
	{"secrets.add", "Add Secret"},
	{"secrets.addNote", "Add Note"},
	{"secrets.lock", "Lock Secrets"},
	{"secrets.export", "Export Secrets"},
	{"secrets.import", "Import Secrets"},
	{"agent.share", "Share with an Agent"},
	{"agent.hand", "Share Pane with Agent"},
	{"agent.take", "Stop Sharing Pane"},
	{"agent.permissions", "Agent Permissions"},
	{"agent.typed", "Typing History"},
	{"serve.window", "Serve This Window"},
	{"serve.attach", "Connect to Window"},
	{"conn.disconnect", "Disconnect"},
	{"pane.titles", "Pane Titles"},
	{"view.fullScreen", "Full Screen"},
	{"view.pin", "Always on Top"},
	{"pane.typeAll", "Type in All Panes"},
	{"conn.command", "Run Command"},
	{"pane.scrollback", "Find in Scrollback"},
	{"sidebar.focus", "Go to Servers"},
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
	{"app.about", "About kakel"},
	{"app.tray", "Tray Icon"},
	{"app.launcher", "Open Launcher"},
	{"app.launcherKey", "Launcher Key"},
	{"app.thisComputer", "This Computer"},
	{"app.install", "Install kakel"},
	{"app.updates", "Updates"},
	{"app.autostart", "Start with Computer"},
	{"sshkey.make", "New SSH Key"},
	{"sshkey.lock", "Lock SSH Keys"},
	{"sshkey.forget", "Remove Saved Key"},
	{"files.copies", "Saved Copies"},
	{"view.jobs", "Show Jobs"},
	{"view.log", "Window Log"},
	{"conn.log", "Connection Log"},
	{"conn.tunnel", "Open Tunnel"},
	{"conn.socks", "Open SOCKS Proxy"},
	{"edit.copy", "Copy"},
	{"edit.selectAll", "Select All"},
	{"edit.paste", "Paste"},
	{"edit.pasteImage", "Paste Image as File"},
	{"view.scrollUp", "Scroll Page Up"},
	{"view.scrollDown", "Scroll Page Down"},
	{"menu.open", "Open the Menus"},
	{"app.exit", "Exit"},
}

// itemPrefixes start the ids of commands on one thing of many: a saved
// server, a machine, a folder, a shell, a saved command or tunnel. They
// are gridterm's, so a shortcuts file written for gridterm works here.
var itemPrefixes = []string{
	"server.open.", "server.edit.", "server.remove.", "conn.log.",
	"conn.terminal.", "conn.files.", "conn.saved.", "conn.savedtunnel.",
	shellfind.CommandPrefix, "shell.pick.", "font.use.",
}

// isItem reports whether id names a command on one thing of many.
func isItem(id string) bool {
	for _, p := range itemPrefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// aliases are gridterm's other names for commands, so a shortcuts file
// written for gridterm runs here as it did there.
var aliases = map[string]string{
	// gridterm's New Terminal beside the focused pane, which here opens
	// on the stage as New Terminal does.
	"pane.open": "conn.terminal",
}

// commandIcons are the icons the menus and the palette show beside a
// command.
var commandIcons = map[string]*icon.Icon{
	"conn.terminal":          icon.SquareTerminal,
	"shell.default":          icon.SquareTerminal,
	"pane.splitRight":        icon.Columns2,
	"pane.splitDown":         icon.Rows2,
	"pane.popOut":            icon.SquareArrowOutUpRight,
	"pane.close":             icon.X,
	"pane.next":              icon.Redo2,
	"pane.previous":          icon.Undo2,
	"pane.titles":            icon.PanelTop,
	"pane.nextInSidebar":     icon.ArrowDown,
	"tab.next":               icon.ArrowRight,
	"tab.previous":           icon.ArrowLeft,
	"tab.close":              icon.X,
	"tab.moveLeft":           icon.ChevronsLeft,
	"tab.newWindow":          icon.AppWindow,
	"servers.window":         icon.Server,
	"secrets.window":         icon.Lock,
	"tab.moveRight":          icon.ChevronsRight,
	"pane.previousInSidebar": icon.ArrowUp,
	"view.switcher":          icon.LayoutGrid,
	"pane.rename":            icon.Pencil,
	"sidebar.toggle":         icon.PanelLeft,
	"sidebar.focus":          icon.PanelLeftOpen,
	"view.theme":             icon.Palette,
	"app.exit":               icon.LogOut,
	"menu.open":              icon.Menu,
	"edit.copy":              icon.Copy,
	"edit.selectAll":         icon.TextSelect,
	"edit.paste":             icon.ClipboardPaste,
	"edit.pasteImage":        icon.ImagePlus,
	"pane.scrollback":        icon.TextSearch,
	"view.fullScreen":        icon.Maximize,
	"view.pin":               icon.Pin,
	"pane.typeAll":           icon.Keyboard,
	"view.jobs":              icon.ListChecks,
	"font.increase":          icon.AArrowUp,
	"font.decrease":          icon.AArrowDown,
	"font.reset":             icon.ALargeSmall,
	"view.scrollUp":          icon.ChevronsUp,
	"view.scrollDown":        icon.ChevronsDown,
	"conn.clearFinished":     icon.ListX,
	"conn.command":           icon.SquareChevronRight,
	"conn.files":             icon.Folder,
	"conn.tunnel":            icon.Cable,
	"conn.socks":             icon.Network,
	"files.goTo":             icon.FolderSearch,
	"files.icons":            icon.LayoutGrid,
	"files.copies":           icon.Bookmark,
	"conn.log":               icon.ScrollText,
	"shell.setup":            icon.Wrench,
	"conn.disconnect":        icon.Unplug,
	"server.editThis":        icon.Pencil,
	"server.forget":          icon.Trash2,
	"agent.share":            icon.Bot,
	"agent.permissions":      icon.ShieldCheck,
	"agent.typed":            icon.History,
	"serve.window":           icon.ScreenShare,
	"serve.attach":           icon.Plug,
	"secrets.open":           icon.Vault,
	"secrets.add":            icon.Plus,
	"secrets.addNote":        icon.StickyNote,
	"secrets.export":         icon.Upload,
	"secrets.import":         icon.Download,
	"secrets.lock":           icon.Lock,
	"sshkey.make":            icon.KeyRound,
	"sshkey.forget":          icon.KeySquare,
	"sshkey.lock":            icon.LockKeyhole,
	"shell.termProgram":      icon.IdCard,
	"view.themesStart":       icon.FilePlus,
	"shortcuts.write":        icon.FilePlus,
	"view.themesReload":      icon.RefreshCw,
	"shortcuts.reload":       icon.RefreshCw,
	"server.reload":          icon.RefreshCw,
	"help.files":             icon.FolderCog,
	"palette.open":           icon.Command,
	"help.shortcuts":         icon.Keyboard,
	"view.log":               icon.ScrollText,
	"app.about":              icon.Info,
	"app.tray":               icon.PanelBottom,
	"app.launcher":           icon.Search,
	"app.launcherKey":        icon.Keyboard,
	"app.thisComputer":       icon.Monitor,
	"app.install":            icon.Download,
	"app.updates":            icon.RefreshCw,
	"app.autostart":          icon.Power,
	"server.connect":         icon.Plug,
	"server.add":             icon.Plus,
	"server.import":          icon.FileInput,
	"secrets.pane":           icon.Vault,
	"secrets.change":         icon.Pencil,
	"secrets.forget":         icon.Trash2,
	"secrets.addKey":         icon.KeyRound,
	"secrets.addPassphrase":  icon.RectangleEllipsis,
	"secrets.removeKey":      icon.Trash2,
	"agent.hand":             icon.Bot,
	"agent.take":             icon.BotOff,
	"sidebar.closeRow":       icon.X,
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
		{id: "edit.selectAll", title: "Select All"},
		{id: "edit.pasteImage", title: "Paste Image as File"},
		{id: "pane.scrollback", title: "Find in Scrollback…", group: true},
	}},
	{"View", []menuItem{
		{id: "sidebar.toggle", title: "Servers"},
		{id: "servers.window", title: "Open Servers Window"},
		{id: "pane.titles", title: "Pane Titles"},
		{id: "view.fullScreen", title: "Full Screen"},
		{id: "view.pin", title: "Always on Top"},
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
		{id: "view.switcher", title: "All Panes…"}, {id: "sidebar.focus", title: "Servers"},
		{id: "pane.typeAll", title: "Type in All Panes", group: true},
		{id: "pane.rename", title: "Rename…"},
		{id: "conn.clearFinished", title: "Clear Finished"},
	}},
	{"Tab", []menuItem{
		{id: "conn.terminal", title: "New Tab"},
		{title: "Go To", caption: true},
		{id: "tab.next", title: "Next"}, {id: "tab.previous", title: "Previous"},
		{title: "Move", caption: true},
		{id: "tab.moveLeft", title: "Left"}, {id: "tab.moveRight", title: "Right"},
		{id: "pane.popOut", title: "Pane to New Tab"},
		{id: "tab.newWindow", title: "Tab to New Window"},
		{id: "tab.close", title: "Close Tab", group: true},
	}},
	{"Machine", []menuItem{
		{title: "Open Here", caption: true},
		{id: "conn.terminal", title: "Terminal"}, {id: "conn.command", title: "Command…"},
		{id: "conn.files", title: "Files"},
		{id: "conn.tunnel", title: "Tunnel…"}, {id: "conn.socks", title: "SOCKS Proxy…"},
		{id: "files.goTo", title: "Go to Directory…", group: true},
		{id: "files.icons", title: "Icon View"},
		{id: "files.copies", title: "Saved Copies…"},
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
		{id: "agent.typed", title: "Typing History"},
		{title: "With Another Window", caption: true},
		{id: "serve.window", title: "Serve This Window…"}, {id: "serve.attach", title: "Connect to Window…"},
	}},
	{"Secrets", []menuItem{
		{id: "secrets.open", title: "Show Secrets"},
		{id: "secrets.window", title: "Open Secrets Window"},
		{id: "secrets.add", title: "Add Secret…"}, {id: "secrets.addNote", title: "Add Note…"},
		{id: "secrets.export", title: "Export…", group: true}, {id: "secrets.import", title: "Import…"},
		{id: "secrets.lock", title: "Lock", group: true},
		{title: "SSH Keys", caption: true},
		{id: "sshkey.make", title: "New SSH Key…"}, {id: "sshkey.lock", title: "Lock SSH Keys"},
		{id: "sshkey.forget", title: "Remove Saved Key…"},
	}},
	{"Options", []menuItem{
		{id: "view.theme", title: "Theme…"},
		{id: "shell.termProgram", title: "Terminal Identity…"},
		{title: "Start a File", caption: true},
		{id: "view.themesStart", title: "Themes"}, {id: "shortcuts.write", title: "Shortcuts"},
		{title: "Read Again", caption: true},
		{id: "view.themesReload", title: "Themes"}, {id: "shortcuts.reload", title: "Shortcuts"},
		{id: "server.reload", title: "Server List"},
		{id: "app.thisComputer", title: "This Computer…", group: true},
		{id: "app.tray", title: "Tray Icon", group: true},
		{id: "app.launcherKey", title: "Launcher Key…"},
		{id: "app.install", title: "Install kakel…", group: true},
		{id: "app.updates", title: "Updates…"},
		{id: "app.autostart", title: "Start with Computer"},
		{id: "help.files", title: "File Locations…", group: true},
	}},
	// The Font menu is made from the families found here.
	{"Font", nil},
	{"Help", []menuItem{
		{id: "palette.open", title: "All Commands…"},
		{id: "help.shortcuts", title: "Shortcuts and Commands"},
		{id: "view.log", title: "Window Log", group: true},
		{id: "app.about", title: "About kakel", group: true},
	}},
}

// commandAlso are other words a command is found by in the palette, as
// people ask for it: "quit" for Exit, "zoom in" for a larger font.
var commandAlso = map[string][]string{
	"font.increase":         {"zoom in", "bigger", "larger"},
	"font.decrease":         {"zoom out", "smaller"},
	"font.reset":            {"zoom", "default", "actual size"},
	"edit.pasteImage":       {"picture", "screenshot", "path"},
	"secrets.pane":          {"secrets", "password", "vault", "note", "overview"},
	"secrets.open":          {"secrets", "password", "vault", "note", "credential"},
	"secrets.add":           {"password", "vault", "keep", "new"},
	"secrets.addNote":       {"secret", "vault", "recovery", "licence", "license", "keep"},
	"secrets.change":        {"password", "vault", "note", "edit", "rename"},
	"secrets.addKey":        {"let another key open the secrets", "vault", "password"},
	"secrets.addPassphrase": {"a way back in when every key is gone", "vault"},
	"secrets.removeKey":     {"stop a key opening the secrets", "vault", "password"},
	"secrets.export":        {"take the secrets somewhere else", "csv", "vault"},
	"secrets.import":        {"bring secrets in from another manager", "csv"},
	"secrets.forget":        {"forget", "password", "vault", "note", "delete"},
	"pane.scrollback":       {"search", "history", "buffer", "save", "view", "open"},
	"view.scrollUp":         {"back", "scrollback"},
	"view.scrollDown":       {"forward", "scrollback"},
	"pane.splitRight":       {"vertical"},
	"pane.splitDown":        {"horizontal"},
	"pane.popOut":           {"unsplit", "detach", "take out of its split", "new tab", "tab"},
	"tab.next":              {"switch", "right"},
	"tab.previous":          {"switch", "left"},
	"tab.moveRight":         {"reorder", "shift"},
	"tab.moveLeft":          {"reorder", "shift"},
	"tab.close":             {"every pane in it"},
	"tab.newWindow":         {"detach", "tear off", "pop out", "own window"},
	"servers.window":        {"tool window", "machines", "sidebar", "own window"},
	"secrets.window":        {"tool window", "password", "vault", "own window"},
	"conn.terminal":         {"pane", "shell", "like this one", "same shell", "same server", "duplicate", "clone", "new tab"},
	"view.fullScreen":       {"fill", "maximise", "maximize", "hide the menus"},
	"view.pin":              {"pin", "keep on top", "float", "stay on top"},
	"pane.typeAll":          {"broadcast", "synchronize", "sync panes", "every pane", "same input"},
	"app.exit":              {"quit", "close this window"},
	"app.about":             {"version"},
	"app.tray":              {"system tray", "notification area", "keep running", "background", "close to tray"},
	"app.launcher":          {"find a machine", "search", "quick", "connect", "global", "hot key"},
	"app.launcherKey":       {"global shortcut", "hot key", "shift+win+k", "change"},
	"app.thisComputer":      {"local", "start folder", "starting directory", "working directory", "default shell", "settings"},
	"app.install":           {"setup", "start menu", "shortcut", "programs"},
	"app.updates":           {"update", "upgrade", "new version", "automatic"},
	"app.autostart":         {"start with windows", "login", "startup", "boot", "tray"},
	"shell.default":         {"pane"},
	"server.connect":        {"ssh", "host", "machine"},
	"server.add":            {"new", "save"},
	"server.import":         {"ssh config", "~/.ssh/config", "openssh", "hosts"},
	"server.reload":         {"reread"},
	"files.copies":          {"remembered", "file", "again", "repeat"},
	"serve.window":          {"share", "listen", "remote"},
	"serve.attach":          {"another", "attach", "take over", "remote", "share panes"},
	"agent.hand":            {"hand over", "add this pane to the share"},
	"agent.take":            {"take this pane out of the share", "remove", "unshare"},
	"agent.share":           {"show the share", "code", "prompt", "skill", "setup"},
	"agent.typed":           {"what the agent typed", "input", "sent"},
	"view.theme":            {"colour", "color", "colors", "scheme"},
	"view.themesReload":     {"colour", "color", "colors", "reread"},
	"view.themesStart":      {"colour", "color", "colors", "write", "create", "edit"},
	"pane.titles":           {"show", "hide", "toggle", "names", "line"},
	"shell.setup":           {"shell integration", "working directory", "osc 7", "on", "off"},
	"shell.termProgram":     {"term_program", "compatibility", "pictures", "images", "calls itself"},
	"sshkey.make":           {"make", "create", "generate", "keygen", "ed25519"},
	"sshkey.forget":         {"forget", "kept", "delete", "list", "identity", "clear"},
	"sidebar.toggle":        {"show", "hide", "toggle", "connections", "panel", "sidebar", "machines", "window"},
	"sidebar.focus":         {"go to the connections", "panel", "focus sidebar", "machines"},
	"conn.command":          {"here", "program", "execute"},
	"conn.tunnel":           {"forward", "port", "local", "remote"},
	"conn.socks":            {"tunnel", "dynamic"},
	"conn.files":            {"file browser", "sftp", "folder", "directory"},
	"files.goTo":            {"folder", "path", "cd", "drive"},
	"files.icons":           {"thumbnails", "pictures", "grid", "tiles", "details", "list", "view as"},
	"conn.disconnect":       {"close the connection", "machine", "server", "log out"},
	"conn.log":              {"how this was reached", "route", "hops"},
	"help.shortcuts":        {"keys", "keyboard", "help"},
	"help.files":            {"where kakel keeps its files", "settings", "config"},
	"view.log":              {"show what the window has logged", "debug", "errors"},
	"shortcuts.write":       {"write", "starting", "keyboard", "create"},
	"shortcuts.reload":      {"keyboard", "reread"},
	"view.switcher":         {"overview", "every pane", "grid"},
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
		return app.SplitPane{}, true
	case "pane.splitDown":
		return app.SplitPane{Vertical: true}, true
	case "pane.popOut":
		return app.PopOut{}, true
	case "pane.close":
		return app.ClosePane{}, true
	case "edit.pasteImage":
		return app.PasteImageAsFile{}, true
	case "tab.next":
		return app.NextTab{}, true
	case "tab.previous":
		return app.NextTab{Back: true}, true
	case "tab.moveRight":
		return app.ShiftTab{}, true
	case "tab.moveLeft":
		return app.ShiftTab{Back: true}, true
	case "tab.close":
		return app.CloseTab{}, true
	case "app.tray":
		return app.ToggleTray{}, true
	case "app.launcher":
		return app.OpenLauncher{}, true
	case "app.autostart":
		return app.ToggleAutostart{}, true
	case "pane.nextInSidebar":
		return app.NextPane{}, true
	case "pane.previousInSidebar":
		return app.NextPane{Back: true}, true
	case "conn.terminal":
		return app.NewTerminal{}, true
	case "shell.default":
		return app.OpenDefaultShell{}, true
	case "secrets.pane":
		return app.ShowSecrets{}, true
	case "secrets.addKey":
		return app.AddSecretsKey{}, true
	case "sidebar.toggle":
		return app.ToggleServers{}, true
	case "app.exit":
		return app.Exit{}, true
	case "conn.files":
		return app.OpenFiles{}, true
	case "view.jobs":
		return app.ShowJobs{}, true
	case "files.copies":
		return app.ShowCopies{}, true
	case "server.reload":
		return app.ReloadServers{}, true
	case "shell.setup":
		return app.ToggleShellSetup{}, true
	case "sshkey.lock":
		return app.LockKeys{}, true
	case "shortcuts.reload":
		return app.ReloadShortcuts{}, true
	case "view.themesReload":
		return app.ReloadThemes{}, true
	case "view.themesStart":
		return app.WriteThemeFile{}, true
	case "conn.clearFinished":
		return app.ClearFinished{}, true
	case "secrets.open":
		return app.ShowSecrets{}, true
	case "secrets.lock":
		return app.LockSecrets{}, true
	case "view.log":
		return app.ShowLog{}, true
	case "font.increase":
		return app.FontSize{Step: 1}, true
	case "font.decrease":
		return app.FontSize{Step: -1}, true
	case "font.reset":
		return app.FontSize{}, true
	}
	return nil, false
}
