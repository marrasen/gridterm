package main

import (
	"context"

	"github.com/marrasen/gridterm/ui"
)

// This file is the dialling state machine: what a connection being made
// is, and the dialog about one already on its way. The names it holds
// while it runs belong to the machines type, in machines.go.

// dialling is a connection being made, and the names the window holds
// while it is.
type dialling struct {
	cancel context.CancelFunc

	// names is every machine of the route and the name the pane goes by.
	names []string

	// made names the machines of the route that answered, in the order
	// they did.
	made []string

	// waiting are requests to run once this connection is made or
	// fails, for a user who asked for the same machine while it was on
	// its way and chose to wait.
	waiting []func()

	// settled says the dial has come back, so anything waiting on it has
	// already run and a new request must run now rather than queue.
	settled bool

	// say writes into the pane watching this connection, so a request
	// that was thrown away says so where the user is looking.
	say func(string)

	// renamed maps what a machine was called when the dial started to
	// what it is called now, for one renamed while it was on its way.
	// The dial goroutine holds the old names and cannot be told.
	renamed map[string]string
}

// nameNow gives what a machine is called now, which is what it was
// called when the dial started unless it has been renamed since.
func (d *dialling) nameNow(was string) string {
	if now, ok := d.renamed[was]; ok {
		return now
	}
	return was
}

// renamedTo tells a dial still on its way that a machine of its route is
// called something else now.
func (d *dialling) renamedTo(was, now string) {
	for i := range d.names {
		if d.names[i] == was {
			d.names[i] = now
		}
	}
	for i := range d.made {
		if d.made[i] == was {
			d.made[i] = now
		}
	}
	// The dial goroutine holds the name it started with, so it is told
	// this way rather than by writing into what it is reading.
	if d.renamed == nil {
		d.renamed = map[string]string{}
	}
	// A machine renamed twice: what the dial started with maps to the
	// name it has now, not to the one in between.
	for started, then := range d.renamed {
		if then == was {
			d.renamed[started] = now
		}
	}
	d.renamed[was] = now
}

// askAboutTheOneOnItsWay asks what to do about a machine that is
// already being connected to.
//
// Which of the two goes is the user's to say: the one on its way may be
// a second from done, or may be stuck on a machine that will never
// answer.
func (a *app) askAboutTheOneOnItsWay(d *dialling, name string, again func()) {
	// Posted, not shown from here. This can be reached from a button of
	// another dialog, and that dialog closes as soon as the button
	// returns, taking anything stacked on top of it.
	a.pump.post(func() { a.showTheOneOnItsWay(d, name, again) })
}

// showTheOneOnItsWay is the dialog itself, on the goroutine that draws.
func (a *app) showTheOneOnItsWay(d *dialling, name string, again func()) {
	f := a.newConfirm("Already connecting to "+name, []string{
		"gridterm is still connecting to " + name + ".",
		"",
		"Two connections to one machine at once would leave one of them" +
			" open with nothing holding it. So either this waits for that" +
			" one, or that one goes.",
	})
	f.AddButton(ui.Button{Title: "Wait for it", Do: func() error {
		if d.settled {
			// It came back while the dialog was open, so there is
			// nothing left to wait for.
			a.pump.post(again)
			return nil
		}
		d.waiting = append(d.waiting, again)
		return nil
	}})
	f.AddButton(ui.Button{Title: "Give up on that one", Do: func() error {
		a.machines.giveUp(d)
		// Not from here: this dialog closes as soon as this returns, and
		// closing one takes anything stacked on top of it.
		a.pump.post(again)
		return nil
	}})
	f.AddButton(ui.Button{Title: "Leave it"})
	a.showForm(f, nil)
}
