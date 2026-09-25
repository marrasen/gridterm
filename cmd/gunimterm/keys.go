package main

import (
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
		{Key: input.KeyD, Mods: input.ModCtrl | input.ModShift}:        "pane.splitRight",
		{Key: input.KeyE, Mods: input.ModCtrl | input.ModShift}:        "pane.splitDown",
		{Key: input.KeyU, Mods: input.ModCtrl | input.ModShift}:        "pane.popOut",
		{Key: input.KeyW, Mods: input.ModCtrl | input.ModShift}:        "pane.close",
		{Key: input.KeyTab, Mods: input.ModCtrl}:                       "pane.next",
		{Key: input.KeyTab, Mods: input.ModCtrl | input.ModShift}:      "pane.previous",
		{Key: input.KeyPageDown, Mods: input.ModCtrl}:                  "pane.nextInSidebar",
		{Key: input.KeyPageUp, Mods: input.ModCtrl}:                    "pane.previousInSidebar",
		{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}:        "conn.terminal",
		{Key: input.KeyB, Mods: input.ModCtrl | input.ModShift}:        "sidebar.toggle",
		{Key: input.KeyV, Mods: input.ModCtrl | input.ModShift}:        "edit.paste",
		{Key: input.KeyInsert, Mods: input.ModShift}:                   "edit.paste",
		{Key: input.KeyPageUp, Mods: input.ModShift}:                   "view.scrollUp",
		{Key: input.KeyPageDown, Mods: input.ModShift}:                 "view.scrollDown",
	})
	return keys
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
	}
	return nil, false
}
