package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/marrasen/kakel/remote"
)

// Connecting to servers, with kakel's remote package, and answering
// what it asks through the window.

// Ask is a question a connection is waiting on the user for, shown as
// a dialog: a title and text in the window's own words, fields to fill
// in, and the words on the button that says yes.
type Ask struct {
	ID      uint64
	Title   string
	Text    string
	Prompts []string
	// Secret says which fields hide what is typed.
	Secret []bool
	// Choose offers answers as buttons, the first the one Enter gives,
	// answered after the fields by the button's words; Yes names the
	// button when there is no Choose. Also is a box to tick, answered
	// after that by "yes" when ticked. No names the button that says
	// no, Cancel when empty.
	Choose []string
	Also   string
	Yes    string
	No     string
	// Actions are buttons that do something and leave the question
	// open, answered by AskAction; Copy is what a Copy button copies,
	// and Link what Open Link opens.
	Actions    []string
	Copy, Link string
	// Danger marks a question whose yes can do harm, and colours its
	// button so. Careful opens on Cancel without the colour. Plain has
	// Yes alone, for something only told.
	Danger, Careful, Plain bool
	// win is the window it is asked in.
	win int
	// Icon is the Lucide name of the icon before the title, one of
	// askIcons, or empty for none; a Danger question shows a warning.
	Icon string
}

// errDeclined is the user saying no to a question, which stops the
// connection quietly.
var errDeclined = errors.New("kakel: declined")

// connect connects to a server, through the jump hosts a saved one
// names, and opens a shell there once it is connected.
func (a *app) connect(in ConnectTo) error { return a.connectThen(in, nil) }

// connectThen connects to a server and then runs then, on the
// program's goroutine, with why it could not connect or nil; or opens a
// shell there when then is nil.
func (a *app) connectThen(in ConnectTo, then func(error)) error {
	var hops []remote.Config
	// names are the hops' names, the saved servers they are, and empty
	// for a typed target.
	var names []string
	name := in.Saved
	if in.Saved != "" {
		if a.book == nil {
			return fmt.Errorf("kakel: no saved servers")
		}
		hosts, err := a.book.Route(in.Saved)
		if err != nil {
			return err
		}
		// A saved kakel window is connected to as one.
		if last := hosts[len(hosts)-1]; last.Window {
			key := ""
			if len(last.Identities) > 0 {
				key = last.Identities[0]
			}
			return a.connectWindow(ConnectWindow{Addr: last.ServeAddr(), KeyFile: key, Name: last.Name})
		}
		for _, h := range hosts {
			hops = append(hops, h.Config())
			names = append(names, h.Name)
		}
	} else {
		cfg, err := remote.ParseTarget(strings.TrimSpace(in.Target))
		if err != nil {
			return err
		}
		hops = []remote.Config{cfg}
		names = []string{""}
		name = cfg.Target()
	}
	savedID := ""
	if in.Saved != "" {
		savedID = a.serverID(in.Saved)
	}
	if _, ok := a.conns[name]; ok {
		if then != nil {
			then(nil)
			return nil
		}
		return a.open(name, placement{})
	}
	if a.dialing[name] {
		a.askAboutTheOneOnItsWay(in, name, then)
		return nil
	}
	a.dialing[name] = true
	dctx, cancel := context.WithCancel(a.ctx)
	a.dialCancel[name] = cancel
	acct := a.account(name)
	logLine(acct, "", "connecting to "+name)
	logPane := a.watchDial(name)
	began := time.Now()
	for i := range hops {
		hops[i].Ask = newAsker(a, name)
		hops[i].Ring = a.ring
		hops[i].Saying = func(what string) { logLine(acct, "", what) }
		hops[i].Wrong = func(what string) { logLine(acct, badly, what) }
	}
	a.st.Status = "Connecting to " + name + "…"
	// From the nearest hop already connected, so a second server behind
	// a jump host does not sign in to the jump host again.
	start, from := a.hopConnected(names, hops)
	// Held while the dial goes through it, so it does not close under
	// the dial when what else went through it goes.
	a.holdHops(start)
	go func() {
		conn, made, err := dialFrom(dctx, start, from, hops, names)
		a.events <- func() {
			defer a.letHopsGo(start)
			delete(a.dialing, name)
			delete(a.dialCancel, name)
			cancel()
			// Whoever asked for it again waits on this one, and hears
			// how it went once this request has had its turn.
			waiting := a.dialWaiters[name]
			delete(a.dialWaiters, name)
			defer func() {
				for _, w := range waiting {
					w(err)
				}
			}()
			a.st.Status = ""
			if err != nil {
				// The hops reached on the way are no use to anything now.
				for _, h := range made {
					_ = h.Close()
				}
				logLine(acct, badly, "could not connect: "+err.Error())
				if errors.Is(err, context.Canceled) && logPane != "" {
					// Given up on purpose: its log goes with it. A
					// failure leaves the log up, saying why.
					a.closePane(logPane)
				}
				if !errors.Is(err, errDeclined) && !errors.Is(err, context.Canceled) {
					a.failed("Couldn't connect to "+name, err.Error())
					a.problem()
				}
				if then != nil {
					then(err)
				}
				return
			}
			a.conns[name] = conn
			a.connIDs[name] = savedID
			a.keepHops(conn, names, hops, made)
			delete(a.dropped, name)
			a.reached[name] = hops[len(hops)-1].Target()
			logLine(acct, well, "connected in "+time.Since(began).Round(10*time.Millisecond).String())
			a.done()
			go func() {
				err := conn.Wait()
				a.events <- func() {
					a.letHopsGo(conn.Via())
					delete(a.routes, conn)
					if err != nil {
						logLine(acct, badly, "disconnected: "+err.Error())
					} else {
						logLine(acct, "", "disconnected")
					}
					delete(a.conns, name)
					delete(a.connIDs, name)
					if err := a.tunnelsDiedOn(name, a.letGo[name]); err != nil {
						a.failed("Trouble closing the tunnels on "+name, err.Error())
					}
					if f, ok := a.remoteFS[name]; ok {
						_ = f.Close()
						delete(a.remoteFS, name)
						a.forgetFar(name)
					}
					if a.letGo[name] {
						delete(a.letGo, name)
					} else {
						// Gone by itself: its row stays, greyed, until it
						// is cleared.
						a.dropped[name] = true
						a.problem()
					}
					a.notify("Disconnected from "+name, "", "")
				}
			}()
			a.dialed(logPane, name, then == nil)
			if then != nil {
				then(nil)
			}
		}
	}()
	return nil
}

// askAboutTheOneOnItsWay asks what to do about a server already being
// connected to: wait for that one, which then does what was asked; give
// it up and connect again; or drop what was asked.
func (a *app) askAboutTheOneOnItsWay(in ConnectTo, name string, then func(error)) {
	go func() {
		ans, err := a.ask(a.ctx, Ask{Title: "Already connecting to " + name, Choose: []string{"Wait", "Retry"}, No: "Cancel"})
		if err != nil {
			return
		}
		choice := ""
		if len(ans.Answers) > 0 {
			choice = ans.Answers[len(ans.Answers)-1]
		}
		a.events <- func() {
			again := func(error) {
				if err := a.connectThen(in, then); err != nil {
					a.failed("Couldn't connect to "+name, err.Error())
					a.problem()
				}
			}
			if !a.dialing[name] {
				// It came back while the question was up.
				again(nil)
				return
			}
			if choice == "Retry" {
				a.giveUp(name)
			}
			a.dialWaiters[name] = append(a.dialWaiters[name], again)
		}
	}()
}

// ask shows q and waits for the answer, or for ctx to end, which takes
// the question away. It runs on a connection's goroutine.
func (a *app) ask(ctx context.Context, q Ask) (AskAnswered, error) {
	q.ID = a.askIDs.Add(1)
	reply := make(chan AskAnswered, 1)
	a.events <- func() {
		a.replies[q.ID] = reply
		q.win = a.frontID()
		a.st.Asks = append(a.st.Asks, q)
	}
	select {
	case ans := <-reply:
		if !ans.Yes {
			return ans, errDeclined
		}
		return ans, nil
	case <-ctx.Done():
		a.events <- func() { a.dropAsk(q.ID) }
		return AskAnswered{}, ctx.Err()
	}
}

func (a *app) dropAsk(id uint64) {
	delete(a.replies, id)
	for i, q := range a.st.Asks {
		if q.ID == id {
			a.st.Asks = append(a.st.Asks[:i:i], a.st.Asks[i+1:]...)
			return
		}
	}
}

// asker answers the remote package's questions through the window.
type asker struct {
	a *app
	// name is the machine being connected to, whose log and dial a
	// notice belongs to.
	name string
	// asked says a password was asked for on this connection already,
	// so asking again means the last one was refused.
	asked *bool
}

// newAsker asks the user what one connection to name needs to know.
func newAsker(a *app, name string) asker { return asker{a: a, name: name, asked: new(bool)} }

// Passphrase implements [remote.Ask].
func (q asker) Passphrase(ctx context.Context, key remote.LockedKey) (string, error) {
	// The one the secrets keep, the first time round: one the key has
	// just refused is worth nothing twice.
	if key.Wrong == 0 {
		kept := make(chan string, 1)
		select {
		case q.a.events <- func() { kept <- q.a.passphraseInHand(key.Path) }:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		select {
		case pass := <-kept:
			if pass != "" {
				return pass, nil
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	text := "The key " + key.Path + " is locked with a passphrase."
	if key.Wrong > 0 {
		text = "That passphrase did not open " + key.Path + ". Try again."
	}
	ans, err := q.a.ask(ctx, Ask{Title: "Unlock your key", Icon: "key-round", Text: text, Prompts: []string{"Passphrase"}, Secret: []bool{true}, Yes: "Unlock"})
	if err != nil {
		return "", err
	}
	return ans.Answers[0], nil
}

// Password implements [remote.Ask].
func (q asker) Password(ctx context.Context, user, host string) (string, error) {
	// Asked again, the last one was refused, and the question says so
	// rather than opening again with no word of why.
	text := ""
	if q.asked != nil {
		if *q.asked {
			text = "Invalid password."
		}
		*q.asked = true
	}
	ans, err := q.a.ask(ctx, Ask{Title: "Sign in to " + user + "@" + host, Icon: "log-in", Text: text, Prompts: []string{"Password"}, Secret: []bool{true}, Yes: "Sign in"})
	if err != nil {
		return "", err
	}
	return ans.Answers[0], nil
}

// Question implements [remote.Ask]. The server's own words are shown as
// its, under a title in the window's words, so a server cannot pass its
// question off as the window's.
func (q asker) Question(ctx context.Context, rq remote.Question) ([]string, error) {
	var said []string
	for _, s := range []string{rq.Name, rq.Instruction} {
		if s = strings.TrimSpace(s); s != "" {
			said = append(said, s)
		}
	}
	text := ""
	if len(said) > 0 {
		text = "The server says: " + strings.Join(said, " ")
	}
	secret := make([]bool, len(rq.Echo))
	for i, e := range rq.Echo {
		secret[i] = !e
	}
	ans, err := q.a.ask(ctx, Ask{Title: rq.User + "@" + rq.Host + " asks", Icon: "log-in", Text: text, Prompts: rq.Prompts, Secret: secret, Yes: "Answer"})
	if err != nil {
		return nil, err
	}
	return ans.Answers, nil
}

// TrustHostKey implements [remote.Ask].
func (q asker) TrustHostKey(ctx context.Context, k remote.HostKey) (bool, error) {
	text := fmt.Sprintf("%s is new to this computer. Its %s key has the fingerprint %s. Connect only if that matches the one its owner gave you.",
		k.Addr, k.Type(), k.Fingerprint())
	// Careful: it opens on Cancel, as the one question where yes by
	// reflex is the answer that cannot be taken back.
	ans, err := q.a.ask(ctx, Ask{Title: "Trust this server?", Text: text, Yes: "Trust and Connect", Careful: true, Icon: "shield-alert"})
	if errors.Is(err, errDeclined) {
		return false, nil
	}
	return err == nil && ans.Yes, err
}

// saveServer saves a server in the book, and says so.
func (a *app) saveServer(in SaveServer) error {
	if a.book == nil {
		return fmt.Errorf("kakel: the saved servers could not be read")
	}
	if err := a.book.Put(in.Host, in.Under); err != nil {
		return err
	}
	a.st.Saved = a.book.Hosts()
	// Its key is kept, to be offered for the next server.
	if len(in.Host.Identities) > 0 && a.settings != nil {
		if err := a.settings.KeepKey(in.Host.Identities[0], mostKeptKeys); err != nil {
			a.failed("Server saved, but its key was not kept", err.Error())
		}
		a.st.KeyFiles = a.settings.Keys()
	}
	a.worked("Saved "+in.Host.Name, in.Host.Target(), "")
	return nil
}

// removeServer forgets a saved server.
func (a *app) removeServer(name string) error {
	if a.book == nil {
		return fmt.Errorf("kakel: the saved servers could not be read")
	}
	if err := a.book.Remove(name); err != nil {
		return err
	}
	a.st.Saved = a.book.Hosts()
	// What the window holds under the name goes with it, as the
	// question said: a dial on its way, a window, a connection.
	switch {
	case a.giveUp(name):
	case a.windows[name] != nil:
		return a.disconnectWindow(name)
	case a.conns[name] != nil:
		a.letGoOfRiders(a.conns[name])
		return a.conns[name].Close()
	}
	a.worked("Removed "+name, "", "")
	return nil
}

// Jump hosts. A saved server reached through others connects to each in
// turn. Those connections are shared: a second server behind the same
// jump host goes through the connection the first made, rather than
// signing in to the jump host again. One the user connected to
// themselves is theirs, and stays. One made only to go through closes
// once nothing goes through it any more.

// routeOf names the route to the last of hops: each hop's login, in
// order. Two connections reached by the same route are to the same
// machine, as far as the saved servers say.
func routeOf(hops []remote.Config) string {
	var b strings.Builder
	for i, h := range hops {
		if i > 0 {
			b.WriteString(" > ")
		}
		b.WriteString(h.Target())
	}
	return b.String()
}

// hopConnected returns the nearest hop of a route, searching back from
// the far end, that is connected already by that same route, and the
// index of the first hop to dial after it. With none, it returns nil
// and 0. A hop whose saved server has been changed since is not gone
// through: it may be another machine now.
func (a *app) hopConnected(names []string, hops []remote.Config) (*remote.Conn, int) {
	for i := len(names) - 2; i >= 0; i-- {
		n := names[i]
		if n == "" {
			continue
		}
		route := routeOf(hops[:i+1])
		for _, c := range []*remote.Conn{a.conns[n], a.hops[n]} {
			if c != nil && !c.Closed() && a.routes[c] == route {
				return c, i + 1
			}
		}
	}
	return nil, 0
}

// dialFrom dials hops[from:], through start when it is not nil. It
// returns the far end, and the connections it made to the hops before
// it, first to last. An error names the hop it came from, when that is
// not the far end.
func dialFrom(ctx context.Context, start *remote.Conn, from int, hops []remote.Config, names []string) (*remote.Conn, []*remote.Conn, error) {
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

// keepHops keeps conn, reached by hops, and the connections it goes
// through: made, the ones its dial made, for the next route to use, and
// every one under it, counted as used by it.
func (a *app) keepHops(conn *remote.Conn, names []string, hops []remote.Config, made []*remote.Conn) {
	a.routes[conn] = routeOf(hops)
	// The hops made are the last ones before the far end.
	first := len(names) - 1 - len(made)
	for i, h := range made {
		a.routes[h] = routeOf(hops[:first+i+1])
		if n := names[first+i]; n != "" {
			// One reached by a route the saved servers no longer name
			// is still counted by what goes through it, and closes with
			// that; this one is gone through from now on.
			if have := a.hops[n]; have == nil || have.Closed() || a.routes[have] != a.routes[h] {
				a.hops[n] = h
			}
		}
	}
	a.holdHops(conn.Via())
}

// holdHops counts one more use of c and each connection under it.
func (a *app) holdHops(c *remote.Conn) {
	for h := c; h != nil; h = h.Via() {
		a.hopUsers[h]++
	}
}

// letHopsGo counts one use off c and each connection under it, and
// closes each one nothing uses any more that the user did not connect
// to.
func (a *app) letHopsGo(c *remote.Conn) {
	for h := c; h != nil; h = h.Via() {
		a.hopUsers[h]--
		if a.hopUsers[h] > 0 {
			continue
		}
		delete(a.hopUsers, h)
		if a.ownConn(h) {
			continue
		}
		for n, o := range a.hops {
			if o == h {
				delete(a.hops, n)
			}
		}
		delete(a.routes, h)
		go func() { _ = h.Close() }()
	}
}

// ownConn reports whether the user connected to c, as a server of its own.
func (a *app) ownConn(c *remote.Conn) bool {
	for _, o := range a.conns {
		if o == c {
			return true
		}
	}
	return false
}

// letGoOfRiders marks the servers that go through c as let go of on
// purpose, as c is closed: they close with it, and are not connections
// that dropped by themselves.
func (a *app) letGoOfRiders(c *remote.Conn) {
	for n, o := range a.conns {
		for h := o.Via(); h != nil; h = h.Via() {
			if h == c {
				a.letGo[n] = true
			}
		}
	}
}
