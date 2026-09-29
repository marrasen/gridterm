package machines

import (
	"context"
	"fmt"
	"strings"

	"github.com/marrasen/kakel/remote"
)

// Jump hosts. A saved server reached through others connects to each in
// turn. Those connections are shared: a second server behind the same
// jump host goes through the connection the first made, rather than
// signing in to the jump host again. One the user connected to
// themselves is theirs, and stays. One made only to go through closes
// once nothing goes through it any more.

// RouteOf names the route to the last of hops: each hop's login, in
// order, and whether it carries this machine's SSH agent. Two
// connections reached by the same route are to the same machine, with
// the same agent, as far as the saved servers say.
func RouteOf(hops []remote.Config) string {
	var b strings.Builder
	for i, h := range hops {
		if i > 0 {
			b.WriteString(" > ")
		}
		b.WriteString(h.Target())
	}
	// The far end's agent alone: a jump host's is nothing to the
	// servers behind it.
	if n := len(hops); n > 0 && hops[n-1].ForwardAgent {
		b.WriteString(agentMark)
	}
	return b.String()
}

// agentMark ends a route whose far end carries this machine's SSH
// agent.
const agentMark = " (agent)"

// HopConnected returns the nearest hop of a route, searching back from
// the far end, that is connected already by that same route, and the
// index of the first hop to dial after it. With none, it returns nil
// and 0. names are the saved servers of the hops, "" for one that is
// not saved.
func (r *Registry) HopConnected(names []ID, hops []remote.Config) (*remote.Conn, int) {
	// The far end itself, connected already only to go through to
	// another: taken as it is, rather than signed in to again, when it
	// was reached the same way, carrying the agent exactly when asked
	// to now. A hop dialled with the agent carries it too.
	if last := len(names) - 1; last >= 0 && names[last] != "" {
		if c := r.hops[names[last]]; c != nil && !c.Closed() && r.routes[c] == RouteOf(hops) {
			return c, last + 1
		}
	}
	for i := len(names) - 2; i >= 0; i-- {
		n := names[i]
		if n == "" {
			continue
		}
		// Gone through, its agent is nothing to what is behind it.
		route := strings.TrimSuffix(RouteOf(hops[:i+1]), agentMark)
		for _, c := range []*remote.Conn{r.Get(n).Conn, r.hops[n]} {
			if c != nil && !c.Closed() && strings.TrimSuffix(r.routes[c], agentMark) == route {
				return c, i + 1
			}
		}
	}
	return nil, 0
}

// KeepHops keeps conn, reached by hops, and the connections it goes
// through: made, the ones its dial made, for the next route to use, and
// every one under it, counted as used by it.
func (r *Registry) KeepHops(conn *remote.Conn, names []ID, hops []remote.Config, made []*remote.Conn) {
	r.routes[conn] = RouteOf(hops)
	// The hops made are the last ones before the far end.
	first := len(names) - 1 - len(made)
	for i, h := range made {
		r.routes[h] = RouteOf(hops[:first+i+1])
		if n := names[first+i]; n != "" {
			// One reached by a route the saved servers no longer name
			// is still counted by what goes through it, and closes with
			// that; this one is gone through from now on.
			if have := r.hops[n]; have == nil || have.Closed() || r.routes[have] != r.routes[h] {
				r.hops[n] = h
			}
		}
	}
	r.Hold(conn.Via())
}

// Hold counts one more use of c and each connection under it.
func (r *Registry) Hold(c *remote.Conn) {
	for h := c; h != nil; h = h.Via() {
		r.users[h]++
	}
}

// Release counts one use off c and each connection under it, and
// closes each one nothing uses any more that the user did not connect
// to.
func (r *Registry) Release(c *remote.Conn) {
	for h := c; h != nil; h = h.Via() {
		r.users[h]--
		if r.users[h] > 0 {
			continue
		}
		delete(r.users, h)
		if r.own(h) {
			continue
		}
		for n, o := range r.hops {
			if o == h {
				delete(r.hops, n)
			}
		}
		delete(r.routes, h)
		go func() { _ = h.Close() }()
	}
}

// Closed forgets the route of c, which has closed.
func (r *Registry) Closed(c *remote.Conn) {
	delete(r.routes, c)
}

// own reports whether the user connected to c, as a server of its own.
func (r *Registry) own(c *remote.Conn) bool {
	for _, m := range r.all {
		if m.Conn == c {
			return true
		}
	}
	return false
}

// LetGoOfRiders marks the servers that go through c as let go of on
// purpose, as c is closed: they close with it, and are not connections
// that dropped by themselves.
func (r *Registry) LetGoOfRiders(c *remote.Conn) {
	for _, m := range r.all {
		if m.Conn == nil {
			continue
		}
		for h := m.Conn.Via(); h != nil; h = h.Via() {
			if h == c {
				m.LetGo = true
			}
		}
	}
}

// Hops are the connections made only to go through.
func (r *Registry) Hops() []*remote.Conn {
	var out []*remote.Conn
	for _, c := range r.hops {
		out = append(out, c)
	}
	return out
}

// SavedOtherwise says, as an error, that the connection to saved server
// id was reached by another route than the one saved now: to another
// address, or with another setting for the SSH agent. Opening on it is
// refused, saying to disconnect it first.
func (r *Registry) SavedOtherwise(id ID) error {
	m := r.Get(id)
	was := r.routes[m.Conn]
	b := r.book()
	if b == nil || m.Conn == nil || was == "" || m.SavedID == "" {
		// Typed, not saved, or from before routes were kept.
		return nil
	}
	hosts, err := b.RouteID(string(id))
	if err != nil {
		return nil
	}
	var hops []remote.Config
	for _, h := range hosts {
		hops = append(hops, h.Config())
	}
	if now := RouteOf(hops); now != was {
		return fmt.Errorf("%s is connected as it was saved before, to %s, and is saved as %s now. Disconnect it first", r.Name(id), was, now)
	}
	return nil
}

// DialFrom dials hops[from:], through start when it is not nil. It
// returns the far end, and the connections it made to the hops before
// it, first to last. An error names the hop it came from, when that is
// not the far end.
func DialFrom(ctx context.Context, start *remote.Conn, from int, hops []remote.Config, names []string) (*remote.Conn, []*remote.Conn, error) {
	var made []*remote.Conn
	conn := start
	for i := from; i < len(hops); i++ {
		var next *remote.Conn
		var err error
		if conn == nil {
			next, err = remote.Connect(ctx, hops[i])
		} else {
			next, err = conn.Through(ctx, hops[i])
		}
		if err != nil {
			if i < len(hops)-1 && names[i] != "" {
				err = fmt.Errorf("through %s: %w", names[i], err)
			}
			return nil, made, err
		}
		if i < len(hops)-1 {
			made = append(made, next)
		}
		conn = next
	}
	return conn, made, nil
}
