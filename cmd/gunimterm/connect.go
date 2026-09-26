package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/marrasen/gridterm/remote"
)

// Connecting to servers, with gridterm's remote package, and answering
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
	// Danger marks a question whose yes can do harm, and colours its
	// button so. Plain has Yes alone, for something only told.
	Danger, Plain bool
}

// errDeclined is the user saying no to a question, which stops the
// connection quietly.
var errDeclined = errors.New("gunimterm: declined")

// connect connects to a server, through the jump hosts a saved one
// names, and opens a shell there once it is connected.
func (a *app) connect(in ConnectTo) error { return a.connectThen(in, nil) }

// connectThen connects to a server and then runs then, on the
// program's goroutine, with why it could not connect or nil; or opens a
// shell there when then is nil.
func (a *app) connectThen(in ConnectTo, then func(error)) error {
	var hops []remote.Config
	name := in.Saved
	if in.Saved != "" {
		if a.book == nil {
			return fmt.Errorf("gunimterm: no saved servers")
		}
		hosts, err := a.book.Route(in.Saved)
		if err != nil {
			return err
		}
		// A saved gridterm window is connected to as one.
		if last := hosts[len(hosts)-1]; last.Window {
			key := ""
			if len(last.Identities) > 0 {
				key = last.Identities[0]
			}
			return a.connectWindow(ConnectWindow{Addr: last.ServeAddr(), KeyFile: key, Name: last.Name})
		}
		for _, h := range hosts {
			hops = append(hops, h.Config())
		}
	} else {
		cfg, err := remote.ParseTarget(strings.TrimSpace(in.Target))
		if err != nil {
			return err
		}
		hops = []remote.Config{cfg}
		name = cfg.Target()
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
	began := time.Now()
	for i := range hops {
		hops[i].Ask = asker{a}
		hops[i].Ring = a.ring
		hops[i].Saying = func(what string) { logLine(acct, "", what) }
		hops[i].Wrong = func(what string) { logLine(acct, badly, what) }
	}
	a.st.Status = "Connecting to " + name + "…"
	go func() {
		conn, err := remote.Connect(dctx, hops[0])
		for _, hop := range hops[1:] {
			if err != nil {
				break
			}
			conn, err = conn.Through(dctx, hop)
		}
		a.events <- func() {
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
				logLine(acct, badly, "could not connect: "+err.Error())
				if !errors.Is(err, errDeclined) && !errors.Is(err, context.Canceled) {
					a.notify("Couldn't connect to "+name, err.Error(), "")
				}
				if then != nil {
					then(err)
				}
				return
			}
			a.conns[name] = conn
			a.reached[name] = hops[len(hops)-1].Target()
			logLine(acct, well, "connected in "+time.Since(began).Round(10*time.Millisecond).String())
			go func() {
				err := conn.Wait()
				a.events <- func() {
					if err != nil {
						logLine(acct, badly, "disconnected: "+err.Error())
					} else {
						logLine(acct, "", "disconnected")
					}
					delete(a.conns, name)
					a.tunnelsDiedOn(name)
					if f, ok := a.remoteFS[name]; ok {
						_ = f.Close()
						delete(a.remoteFS, name)
					}
					a.notify("Disconnected from "+name, "", "")
				}
			}()
			if then != nil {
				then(nil)
				return
			}
			if err := a.open(name, placement{}); err != nil {
				a.notify("Couldn't open a shell on "+name, err.Error(), "")
			}
		}
	}()
	return nil
}

// askAboutTheOneOnItsWay asks what to do about a server already being
// connected to, as gridterm asks: wait for that one, which then does
// what was asked; give it up and connect again; or drop what was asked.
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
					a.notify("Couldn't connect to "+name, err.Error(), "")
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
type asker struct{ a *app }

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
	ans, err := q.a.ask(ctx, Ask{Title: "Unlock your key", Text: text, Prompts: []string{"Passphrase"}, Secret: []bool{true}, Yes: "Unlock"})
	if err != nil {
		return "", err
	}
	return ans.Answers[0], nil
}

// Password implements [remote.Ask].
func (q asker) Password(ctx context.Context, user, host string) (string, error) {
	ans, err := q.a.ask(ctx, Ask{Title: "Sign in to " + user + "@" + host, Prompts: []string{"Password"}, Secret: []bool{true}, Yes: "Sign in"})
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
	ans, err := q.a.ask(ctx, Ask{Title: rq.User + "@" + rq.Host + " asks", Text: text, Prompts: rq.Prompts, Secret: secret, Yes: "Answer"})
	if err != nil {
		return nil, err
	}
	return ans.Answers, nil
}

// TrustHostKey implements [remote.Ask].
func (q asker) TrustHostKey(ctx context.Context, k remote.HostKey) (bool, error) {
	text := fmt.Sprintf("%s is new to this computer. Its %s key has the fingerprint %s. Connect only if that matches the one its owner gave you.",
		k.Addr, k.Type(), k.Fingerprint())
	ans, err := q.a.ask(ctx, Ask{Title: "Trust this server?", Text: text, Yes: "Trust and Connect"})
	if errors.Is(err, errDeclined) {
		return false, nil
	}
	return err == nil && ans.Yes, err
}

// Notice implements [remote.Ask]: the server's message, in a toast.
func (q asker) Notice(_ context.Context, n remote.Notice) {
	body := strings.TrimSpace(strings.Join([]string{n.Name, n.Instruction, n.Text}, " "))
	go func() { q.a.events <- func() { q.a.notify(n.User+"@"+n.Host+" says", body, "") } }()
}

// saveServer saves a server in the book, and says so.
func (a *app) saveServer(in SaveServer) error {
	if a.book == nil {
		return fmt.Errorf("gunimterm: the saved servers could not be read")
	}
	if err := a.book.Put(in.Host, in.Under); err != nil {
		return err
	}
	a.st.Saved = a.book.Hosts()
	a.notify("Saved "+in.Host.Name, in.Host.Target(), "")
	return nil
}

// removeServer forgets a saved server.
func (a *app) removeServer(name string) error {
	if a.book == nil {
		return fmt.Errorf("gunimterm: the saved servers could not be read")
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
		return a.conns[name].Close()
	}
	a.notify("Removed "+name, "", "")
	return nil
}
