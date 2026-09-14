package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui"
)

// panelWidth is how wide the connections panel starts.
const panelWidth = 26

// newPanel builds the list of connections, in the window's colours.
func (a *app) newPanel() *ui.List {
	l := ui.NewList()
	l.Style = ui.ListStyle{
		FG: a.colours.FG,
		BG: a.colours.BG,
		// The selected row is marked the way a selected tab is, so the
		// two read as the same thing.
		SelectedFG: a.colours.BG,
		SelectedBG: a.colours.FG,
		// A machine's name is a heading, not one of its connections.
		HeaderFG: a.colours.ANSI[6],
		// Dimmer than the row: what a connection is doing is a note
		// beside it, not part of its name.
		NoteFG: a.colours.ANSI[8],
	}
	l.OnActivate = func(row ui.ListRow) error { return a.revealRow(row) }
	return l
}

// revealRow puts whatever a row names in front of the user.
func (a *app) revealRow(row ui.ListRow) error {
	e, ok := row.Key.(*conns.Entry)
	if !ok || e.Reveal == nil {
		return nil
	}
	e.Reveal()
	return nil
}

// refreshPanel rebuilds the list from what is open.
//
// Called every frame. The rows are built afresh each time and the list
// keeps the user's place by key, so a connection opening or closing does
// not move the selection out from under them.
//
// Nothing here dirties a layer on its own: a row whose text has not
// changed is written with the same value, and grid.Set leaves a cell
// that did not change alone. A state falls from active to settled by
// itself, because the text is worked out again from the time passed in.
func (a *app) refreshPanel(now time.Time) {
	if a.panel == nil {
		return
	}
	// A terminal names itself: what the program in it called the window
	// is what the panel shows. Read rather than pushed, the way the
	// window title is.
	for t, e := range a.panes {
		e.Label = t.Title()
	}

	var rows []ui.ListRow
	for _, group := range a.registry.Groups(now) {
		rows = append(rows, ui.ListRow{
			Text:   groupName(group.Host),
			Header: true,
			Key:    "host:" + group.Host,
		})
		for _, row := range group.Rows {
			rows = append(rows, a.panelRow(row, now))
		}
	}
	a.panel.SetRows(rows)
}

// panelRow turns one connection into a line.
func (a *app) panelRow(row conns.Row, now time.Time) ui.ListRow {
	text := row.Kind.String()
	if row.Label != "" {
		text += "  " + row.Label
	}
	out := ui.ListRow{Text: text, Depth: 1, Key: row.Entry, Note: a.note(row, now)}
	if row.State == meter.Closed {
		// A finished connection reads as finished rather than as one
		// more thing running.
		out.FG = a.colours.ANSI[8]
	}
	return out
}

// note is the word at the end of a row: what it is doing, and how fast
// when that is worth knowing.
func (a *app) note(row conns.Row, now time.Time) string {
	if row.Note != "" {
		return row.Note
	}
	if row.State == meter.Active && row.Meter != nil {
		rate := a.rates[row.Entry]
		if rate == nil {
			rate = &meter.Rate{}
			a.rates[row.Entry] = rate
		}
		in, out := rate.Sample(row.Meter, now)
		if speed := meter.Speed(max(in, out)); speed != "" {
			return speed
		}
	}
	return row.State.String()
}

// groupName is what a machine is called on the panel.
func groupName(host string) string {
	if host == conns.Local {
		return "Local"
	}
	return host
}

// trackConnection puts something on the panel and hands back what takes
// it off again.
func (a *app) trackConnection(e *conns.Entry) func() {
	a.registry.Add(e)
	return func() {
		a.registry.Drop(e)
		delete(a.rates, e)
	}
}

// showPanel opens or closes the connections panel.
func (a *app) showPanel(on bool) error {
	if a.dock == nil {
		return nil
	}
	a.dock.ShowPanel(on)
	a.relayout()
	return nil
}

// togglePanel shows the panel, or hides it when it is already showing.
func (a *app) togglePanel() error {
	if a.dock == nil {
		return nil
	}
	return a.showPanel(a.dock.Collapsed)
}

// focusPanel puts the keys on the panel, opening it if it is hidden.
func (a *app) focusPanel() error {
	if a.dock == nil {
		return nil
	}
	if err := a.showPanel(true); err != nil {
		return err
	}
	if !a.dock.Focus(a.panel) {
		return errors.New("there is no room for the panel")
	}
	a.markDirty()
	return nil
}

// closeSelectedConnection ends whatever the panel has selected.
func (a *app) closeSelectedConnection() error {
	e, ok := a.selectedConnection()
	if !ok {
		return nil
	}
	if e.Close == nil {
		return fmt.Errorf("%s cannot be closed from here", e.Kind)
	}
	return e.Close()
}

// clearFinished takes every connection that has ended off the panel.
func (a *app) clearFinished() error {
	if a.registry.DropFinished(time.Now()) > 0 {
		a.markDirty()
	}
	return nil
}

// selectedConnection returns what the panel has selected.
func (a *app) selectedConnection() (*conns.Entry, bool) {
	if a.panel == nil {
		return nil, false
	}
	row, ok := a.panel.Selected()
	if !ok {
		return nil, false
	}
	e, ok := row.Key.(*conns.Entry)
	return e, ok
}
