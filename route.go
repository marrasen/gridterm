package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/gridterm/remote"
)

// step is one machine on the way to another: what to connect to, and
// what the window calls it.
type step struct {
	name string
	cfg  remote.Config
	term string
}

// hostStep turns a saved machine into a step of a route.
func hostStep(h remote.Host) step {
	return step{name: h.Name, cfg: h.Config(), term: h.Term}
}

// route returns the machines to connect to in order to reach one: the
// far end last, and whatever it is reached through before it.
//
// A machine already connected to is a route of one, whether or not it
// was ever saved: it is reachable, which is what a route is for.
func (a *app) route(name string) ([]step, error) {
	f := a.about(name)
	if f.machine != nil {
		return []step{f.machine.at}, nil
	}
	hosts, err := a.book.Route(name)
	if err != nil {
		return nil, err
	}
	route := make([]step, len(hosts))
	for i, h := range hosts {
		if h.Window {
			// A window serves gridterm's own protocol and has no shell
			// to log in to. Refused where a route is built rather than
			// only where one is opened: a caller that forgot to ask
			// would otherwise log in to the serve port and find out from
			// the far end refusing a session.
			return nil, fmt.Errorf(
				"%s is a gridterm window, which is taken over rather than logged in to", h.Name)
		}
		route[i] = hostStep(h)
	}
	return route, nil
}

// plan splits a route into the connection it can start from and the
// machines still to be reached.
//
// It runs on the drawing goroutine, which is the only one that may read
// what the window is holding. The dialling happens elsewhere.
//
// The search runs from the far end back, so a machine already connected
// to is used however it was reached.
func (a *app) plan(route []step) (through *machine, missing []step, err error) {
	if len(route) == 0 {
		return nil, nil, errors.New("there is no route to that machine")
	}
	for at, r := range slices.Backward(route) {
		m := a.about(r.name).machine
		if m == nil {
			continue
		}
		if !m.at.cfg.SameMachine(r.cfg) {
			return nil, nil, fmt.Errorf(
				"%q is already connected to %s, which is not %s; close it first",
				m.at.name, m.at.cfg.Target(), r.cfg.Target())
		}
		return m, route[at+1:], nil
	}
	return nil, route, nil
}

// savedWindowInRoute is the saved window a request names, by the name
// it was asked for or by the address its last step would be dialled at.
//
// By address as well as by name, because a connection made from a typed
// target knows nothing about the list. Only the last step: a window
// serves gridterm's own protocol and cannot be a machine on the way to
// another one.
func (a *app) savedWindowInRoute(name string, route []step) (remote.Host, bool) {
	if f := a.about(name); f.serves {
		return f.record(), true
	}
	if len(route) == 0 {
		return remote.Host{}, false
	}
	last := route[len(route)-1]
	if f := a.about(last.name); f.serves {
		return f.record(), true
	}
	addr := last.cfg.Host
	if addr == "" {
		return remote.Host{}, false
	}
	if last.cfg.Port != 0 {
		addr = net.JoinHostPort(last.cfg.Host, strconv.Itoa(last.cfg.Port))
	}
	if h, ok := a.windows.savedAt(addr); ok {
		return h, true
	}
	if last.cfg.Port == 0 {
		// A target typed with no port, so the address on its own is
		// what the list has to match.
		for _, h := range a.book.Hosts() {
			if h.Window && strings.EqualFold(h.Address, addr) {
				return h, true
			}
		}
	}
	return remote.Host{}, false
}

// dialRoute opens each machine of a route in turn, each through the one
// before it, and reports each as soon as it answers.
//
// made is called with every connection the moment it is up, so the
// window can hold it: a machine that answered stays connected even when
// the machine beyond it does not. It is called from this goroutine.
func dialRoute(ctx context.Context, through *remote.Conn, route []step, made func(int, *remote.Conn)) error {
	for i, s := range route {
		// Checked between hops as well as inside each one, so giving up
		// does not start a login on the next machine along.
		if err := ctx.Err(); err != nil {
			return err
		}
		var (
			conn *remote.Conn
			err  error
		)
		if through == nil {
			conn, err = remote.Connect(ctx, s.cfg)
		} else {
			conn, err = through.Through(ctx, s.cfg)
		}
		if err != nil {
			if len(route) > 1 {
				// Which machine of the route failed, which the caller
				// cannot work out from the error on its own.
				return fmt.Errorf("%s: %w", s.name, err)
			}
			return err
		}
		made(i, conn)
		through = conn
	}
	return nil
}
