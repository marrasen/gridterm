package main

import (
	"context"

	"github.com/marrasen/gridterm/remote"

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

	// route is the machines being reached and how, so a rename can ask
	// whether the name it is changing still stands for the machine
	// being dialled under it. A rename that changes the address as
	// well leaves the dial where it is, the way it leaves a connection.
	//
	// Empty for a window being taken over, which is reached by its
	// address rather than by a route.
	route []step

	// takeover says this is a window being taken over rather than a
	// machine being dialled, so there is no route to ask about.
	//
	// Said rather than read off an empty route: "no route" would also
	// be what a route this window cannot account for looks like, and
	// the two want opposite answers.
	takeover bool

	// made names the machines of the route that answered, in the order
	// they did.
	made []string

	// waiting are requests to run once this connection is made or
	// fails, for a user who asked for the same machine while it was on
	// its way and chose to wait.
	waiting []func()

	// answering are the callers blocked on this connection, told
	// whichever way it goes: made says the machine answered.
	//
	// Not waiting: what waits is work to run once the machine answers,
	// and a connection that was not made leaves it nothing to do, so it
	// is thrown away. A caller blocked on an answer has to be told
	// anyway, or it waits for a connection that is never coming. What
	// to say about one that failed is the caller's, which is why this
	// carries no error of its own.
	answering []func(made bool)

	// settled says the dial has come back, so anything waiting on it has
	// already run and a new request must run now rather than queue.
	settled bool

	// log is the account being written into the pane that watches this
	// connection, so a request that was thrown away says so where the
	// user is looking and so the account can be opened while it runs.
	log *connLog

	// renamed maps what a machine was called when the dial started to
	// what it is called now, for one renamed while it was on its way.
	// The dial goroutine holds the old names and cannot be told.
	renamed map[string]string
}

// stepFor is how one machine of the route is being reached, and false
// when the route has no such name.
func (d *dialling) stepFor(name string) (step, bool) {
	for _, s := range d.route {
		if s.name == name {
			return s, true
		}
	}
	return step{}, false
}

// movesTo reports whether a machine of this dial, called was, is the
// one a saved entry now describes, so the dial follows the rename.
//
// The same rule the connection gets: a rename that changes the address
// as well says the name stands for a different machine now, so the
// dial keeps the name it was started under.
func (d *dialling) movesTo(was string, to remote.Host) bool {
	if d.takeover {
		// A window, reached by its address. There is no route to ask
		// about and the name is all it goes by, so it follows.
		return true
	}
	s, known := d.stepFor(was)
	// A name this route does not spell is one this window cannot
	// account for, and a dial it cannot account for is one to leave
	// where it is.
	return known && s.cfg.SameMachine(to.Config())
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
	// The route too, because it is what says whether a later rename is
	// still the same machine. Left spelling the old name, the machine
	// would be renamed once more onto an address of somewhere else and
	// the window would have no way to tell.
	for i := range d.route {
		if d.route[i].name == was {
			d.route[i].name = now
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
//
// The title says the whole of it, so there is nothing under it. The
// three buttons are the three things that can be done about a machine
// already being dialled, and each says which.
func (a *app) showTheOneOnItsWay(d *dialling, name string, again func()) {
	f := a.newConfirm(dlgAlreadyConnecting+name, nil)
	// Wait joins the attempt in progress: what was asked for runs once
	// that one has come back, whichever way it does.
	f.AddButton(ui.Button{Title: btnWait, Do: func() error {
		if d.settled {
			// It came back while the dialog was open, so there is
			// nothing left to wait for.
			a.pump.post(again)
			return nil
		}
		d.waiting = append(d.waiting, again)
		return nil
	}})
	// Retry cancels the attempt in progress and dials again, for one
	// stuck on a machine that is never going to answer.
	f.AddButton(ui.Button{Title: btnRetry, Do: func() error {
		a.machines.giveUp(d)
		// Not from here: this dialog closes as soon as this returns, and
		// closing one takes anything stacked on top of it.
		a.pump.post(again)
		return nil
	}})
	// Cancel drops this request and leaves the attempt in progress
	// running: the user asked for something and has changed their mind
	// about it, not about the connection.
	f.AddButton(ui.Button{Title: btnCancel})
	a.showForm(f, nil)
}
