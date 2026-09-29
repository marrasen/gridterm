package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/kakel/machines"

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
	// Preformatted shows Text as it was written, in the fixed-width
	// face, to be selected: a secret, not a sentence.
	Preformatted bool
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
	// names are the hops' IDs, the saved servers they are, and empty for
	// a typed target; shown is what each is called.
	var names []machines.ID
	var shown []string
	name := in.Server
	if in.Server != "" {
		if a.book == nil {
			return fmt.Errorf("kakel: the server list could not be read")
		}
		hosts, err := a.book.RouteID(string(in.Server))
		if err != nil {
			return err
		}
		// A saved kakel window is connected to as one.
		if last := hosts[len(hosts)-1]; last.Window {
			key := ""
			if len(last.Identities) > 0 {
				key = last.Identities[0]
			}
			return a.connectWindow(ConnectWindow{Addr: last.ServeAddr(), KeyFile: key, ID: machines.ID(last.ID)})
		}
		for _, h := range hosts {
			hops = append(hops, h.Config())
			names = append(names, machines.ID(h.ID))
			shown = append(shown, h.Name)
		}
	} else {
		cfg, err := remote.ParseTarget(strings.TrimSpace(in.Target))
		if err != nil {
			return err
		}
		// Typed at a saved window's address: that window, as a window,
		// not SSH to the port it serves on. A quick connection made
		// again stays SSH, as it was made.
		if h, ok := a.savedWindowAt(cfg); ok && in.As == "" {
			return a.connectWindow(ConnectWindow{Addr: h.ServeAddr(), KeyFile: h.KeyFile(), ID: machines.ID(h.ID)})
		}
		hops = []remote.Config{cfg}
		names, shown = []machines.ID{machines.Local}, []string{cfg.Target()}
		// A quick connection, by the ID it has while it is kept.
		name = in.As
		if !a.machines.IsQuick(name) {
			name = a.machines.NewQuick(cfg.Target(), false)
		}
	}
	savedID := in.Server
	called := a.machines.Name(name)
	if _, ok, err := a.connOf(name); ok {
		if err != nil {
			return err
		}
		if then != nil {
			then(nil)
			return nil
		}
		return a.open(name, Placement{})
	}
	if a.machines.Get(name).Dialing != nil {
		a.askAboutTheOneOnItsWay(in, name, then)
		return nil
	}
	dctx, cancel := context.WithCancel(a.ctx)
	a.machines.At(name).Dialing = cancel
	acct := a.dialLog(name)
	logLine(acct, "", "connecting to "+called)
	logPane := a.watchDial(name)
	began := time.Now()
	for i := range hops {
		hops[i].Ask = newAsker(a, name)
		hops[i].Ring = a.ring
		hops[i].Saying = func(what string) { logLine(acct, "", what) }
		hops[i].Wrong = func(what string) { logLine(acct, badly, what) }
	}
	a.showDialling()
	// From the nearest hop already connected, so a second server behind
	// a jump host does not sign in to the jump host again.
	start, from := a.machines.HopConnected(names, hops)
	// Held while the dial goes through it, so it does not close under
	// the dial when what else went through it goes.
	a.machines.Hold(start)
	go func() {
		conn, made, err := machines.DialFrom(dctx, start, from, hops, shown)
		a.events <- func() {
			defer a.machines.Release(start)
			a.machines.At(name).Dialing = nil
			cancel()
			// Whoever asked for it again waits on this one, and hears
			// how it went once this request has had its turn.
			waiting := a.machines.Get(name).Waiters
			a.machines.At(name).Waiters = nil
			defer func() {
				for _, w := range waiting {
					w(err)
				}
			}()
			a.showDialling()
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
					a.failed("Couldn't connect to "+called, err.Error())
					a.problem()
				}
				if then != nil {
					then(err)
				}
				return
			}
			m := a.machines.At(name)
			m.Conn, m.SavedID = conn, savedID
			a.machines.KeepHops(conn, names, hops, made)
			m.Dropped = false
			m.Reached = hops[len(hops)-1].Target()
			logLine(acct, well, "connected in "+time.Since(began).Round(10*time.Millisecond).String())
			a.done()
			go func() {
				err := conn.Wait()
				a.events <- func() {
					a.machines.Release(conn.Via())
					a.machines.Closed(conn)
					if err != nil {
						logLine(acct, badly, "disconnected: "+err.Error())
					} else {
						logLine(acct, "", "disconnected")
					}
					a.machines.At(name).Conn = nil
					a.machines.At(name).SavedID = ""
					if err := a.tunnelsDiedOn(name, a.machines.Get(name).LetGo); err != nil {
						a.failed("Trouble closing the tunnels on "+a.machines.Name(name), err.Error())
					}
					if f := a.machines.Get(name).Files; f != nil {
						_ = f.Close()
						a.machines.At(name).Files = nil
						a.forgetFar(name)
					}
					if a.machines.Get(name).LetGo {
						a.machines.At(name).LetGo = false
					} else {
						// Gone by itself: its row stays, greyed, until it
						// is cleared.
						a.machines.At(name).Dropped = true
						a.problem()
					}
					a.notify("Disconnected from "+a.machines.Name(name), "", "")
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
func (a *app) askAboutTheOneOnItsWay(in ConnectTo, name machines.ID, then func(error)) {
	called := a.machines.Name(name)
	go func() {
		ans, err := a.ask(a.ctx, Ask{Title: "Already connecting to " + called, Choose: []string{"Wait", "Retry"}, No: "Cancel"})
		if err != nil {
			// Whoever asked for it hears it was not.
			if then != nil {
				a.events <- func() { then(errDeclined) }
			}
			return
		}
		choice := ""
		if len(ans.Answers) > 0 {
			choice = ans.Answers[len(ans.Answers)-1]
		}
		a.events <- func() {
			again := func(error) {
				if err := a.connectThen(in, then); err != nil {
					a.failed("Couldn't connect to "+called, err.Error())
					a.problem()
				}
			}
			if a.machines.Get(name).Dialing == nil {
				// It came back while the question was up.
				again(nil)
				return
			}
			if choice == "Retry" {
				a.giveUp(name)
			}
			m := a.machines.At(name)
			m.Waiters = append(m.Waiters, again)
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
	name machines.ID
	// asked says a password was asked for on this connection already,
	// so asking again means the last one was refused.
	asked *bool
}

// newAsker asks the user what one connection to name needs to know.
func newAsker(a *app, name machines.ID) asker { return asker{a: a, name: name, asked: new(bool)} }

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
	// The key it had, to keep the one it has only when that changed.
	var hadKey string
	if old, ok := a.book.Lookup(in.Under); ok && in.Under != "" && len(old.Identities) > 0 {
		hadKey = old.Identities[0]
	}
	// Everything open on it goes by its ID, so a new name is only a
	// new name.
	if err := a.book.Put(in.Host, in.Under); err != nil {
		return err
	}
	a.st.Saved = a.book.Hosts()
	// Its files say what goes wrong by its name, the new one.
	if h, ok := a.book.Lookup(in.Host.Name); ok {
		if f, ok := a.machines.Get(machines.ID(h.ID)).Files.(interface{ Renamed(string) }); ok {
			f.Renamed(h.Name)
		}
	}
	// Its key is kept, to be offered for the next server: a key chosen
	// now, not one it had and was saved with again untouched, which
	// would move to the front of the list for nothing.
	if len(in.Host.Identities) > 0 && in.Host.Identities[0] != hadKey && a.settings != nil {
		if err := a.settings.KeepKey(in.Host.Identities[0], mostKeptKeys); err != nil {
			a.failed("Server saved, but its key was not kept", err.Error())
		}
		a.st.KeyFiles = a.settings.Keys()
	}
	a.worked("Saved "+in.Host.Name, in.Host.Target(), "")
	return nil
}

// removeServer forgets the saved server with ID name.
func (a *app) removeServer(name machines.ID) error {
	if a.book == nil {
		return fmt.Errorf("kakel: the saved servers could not be read")
	}
	called := a.machines.Name(name)
	if err := a.book.RemoveID(string(name)); err != nil {
		return err
	}
	a.st.Saved = a.book.Hosts()
	// Named still by what is left of it, until that goes.
	a.machines.Removed(name, called)
	// What the window holds under the name goes with it, as the
	// question said: a dial on its way, a window, a connection.
	switch {
	case a.machines.Get(name).Dropped:
		// Its connection went already: what it left goes too, the offer
		// to reconnect, which would bring it back, and a reconnect on
		// its way.
		a.giveUp(name)
		a.clearMachine(name)
	case a.giveUp(name):
	case a.machines.Get(name).Window != nil:
		return a.disconnectWindow(name)
	case a.machines.Get(name).Conn != nil:
		// On purpose: no dropped row kept for it to reconnect from.
		a.machines.At(name).LetGo = true
		a.machines.LetGoOfRiders(a.machines.Get(name).Conn)
		return a.machines.Get(name).Conn.Close()
	}
	a.worked("Removed "+called, "", "")
	return nil
}

// connOf is the connection to machine to open something on, and
// whether there is one. One to a saved server changed since, to another
// address or another setting for the SSH agent, is to where it was:
// opening on it is refused, saying to disconnect it first.
func (a *app) connOf(machine machines.ID) (*remote.Conn, bool, error) {
	c := a.machines.Get(machine).Conn
	if c == nil {
		return nil, false, nil
	}
	if err := a.machines.SavedOtherwise(machine); err != nil {
		return nil, true, err
	}
	return c, true, nil
}

// savedWindowAt is the saved window a typed target names by its address:
// with the port it serves on, when one is typed, or by the address
// alone when none is. One typed with a user is an SSH login, as a
// window has none.
func (a *app) savedWindowAt(cfg remote.Config) (remote.Host, bool) {
	if a.book == nil || cfg.Host == "" || cfg.User != "" {
		return remote.Host{}, false
	}
	for _, h := range a.book.Hosts() {
		if !h.Window || !sameHost(h.Address, cfg.Host) {
			continue
		}
		if cfg.Port == 0 || h.ServeAddr() == net.JoinHostPort(h.Address, strconv.Itoa(cfg.Port)) {
			return h, true
		}
	}
	return remote.Host{}, false
}

// sameHost is whether two addresses name the same host: names in any
// case, and IP addresses however they are spelled, in brackets or not.
func sameHost(a, b string) bool {
	a, b = strings.Trim(a, "[]"), strings.Trim(b, "[]")
	if ipA, ipB := net.ParseIP(a), net.ParseIP(b); ipA != nil && ipB != nil {
		return ipA.Equal(ipB)
	}
	return strings.EqualFold(a, b)
}

// showDialling says on the status line what is being connected to: the
// connections on their way, all of them, so one that lands does not
// clear the line while another is still being made. Pictures on their
// way are said after.
func (a *app) showDialling() {
	var names []string
	for _, id := range a.machines.Dialing() {
		names = append(names, a.machines.Name(id))
	}
	var said []string
	switch len(names) {
	case 0:
	case 1:
		said = append(said, "Connecting to "+names[0]+"…")
	default:
		said = append(said, "Connecting to "+strings.Join(names[:len(names)-1], ", ")+" and "+names[len(names)-1]+"…")
	}
	switch {
	case a.sending == 1:
		said = append(said, "Sending a picture to "+a.sendingTo+"…")
	case a.sending > 1:
		said = append(said, fmt.Sprintf("Sending %d pictures…", a.sending))
	}
	a.st.Status = strings.Join(said, "  ·  ")
}
