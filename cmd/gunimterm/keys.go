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
		{Key: input.KeyA, Mods: input.ModCtrl | input.ModShift}: "pane.switch",
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
	{"File", []menuItem{{id: "conn.terminal", title: "New Terminal"}, {id: "pane.close", title: "Close Pane", group: true}, {id: "app.exit", title: "Exit", group: true}}},
	{"Edit", []menuItem{{id: "edit.copy", title: "Copy"}, {id: "edit.paste", title: "Paste"}}},
	{"View", []menuItem{{id: "sidebar.toggle", title: "Sidebar"}, {id: "palette.open", title: "All Commands…", group: true}}},
	{"Pane", []menuItem{
		{id: "pane.splitRight", title: "Split Right"}, {id: "pane.splitDown", title: "Split Down"}, {id: "pane.popOut", title: "Pop Out"},
		{id: "pane.next", title: "Next Pane", group: true}, {id: "pane.previous", title: "Previous Pane"},
		{id: "pane.switch", title: "Switch Pane…"},
		{id: "pane.rename", title: "Rename…", group: true},
	}},
}

type menuItem struct {
	id, title string
	group     bool
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
	}
	return nil, false
}
