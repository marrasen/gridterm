package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui"
)

// errDismissed is what a dialog closed without an answer reports.
var errDismissed = errors.New("cancelled")

// askUser answers a connection's questions with dialogs.
//
// Every method is called from a goroutine that is connecting, and every
// dialog belongs to the goroutine that is drawing. The two meet at the
// pump: the question is posted as work for the next frame, and the
// answer comes back down a channel.
type askUser struct {
	app *app

	// log is the pane the connection is writing its account into, or
	// nil when there is none. What a server says goes there as well as
	// into a dialog: a dialog is read once and dismissed, and a link
	// that has been dismissed is a link nobody can use.
	log *connLog

	// stop gives up on the connection, and is nil when there is no way
	// to. It is called on the goroutine that draws.
	//
	// The closure rather than the machine's name: what a server calls
	// itself in a message is an address, and the window holds the name
	// the user gave it. Looking one up by the other gave up on nothing
	// at all.
	stop func()

	// askedPassword says the account password has been asked for once
	// already, so a second time is one the server refused.
	askedPassword bool
}

// Passphrase asks for the passphrase of a private key file.
//
// A passphrase that did not unlock the key comes back here rather than
// being let through, and the dialog says so. Without that the user typed
// a passphrase, watched the dialog go, and was left to work out from a
// connection that failed some other way -- or did not fail at all --
// that the key had never been offered.
//
// It is asked as often as it takes. Nothing is counted down: the key is
// a file on this machine that this user can already read, so a limit
// protects nothing and only strands whoever mistyped a long passphrase.
func (u *askUser) Passphrase(ctx context.Context, key remote.LockedKey) (string, error) {
	// The one the vault holds, when it holds one and a key already
	// unlocked opens it. Only on the first go round: a passphrase the
	// key has just refused is not worth offering twice, and the user is
	// owed the dialog instead.
	if key.Wrong == 0 {
		if pass, saved := u.app.savedPassphrase(ctx, key.Path); saved {
			return pass, nil
		}
	}
	return u.secret(ctx, secret{
		title:   "Unlock Private Key",
		lines:   []string{key.Path},
		labels:  []string{"Passphrase"},
		masked:  []bool{true},
		accept:  "Unlock",
		trouble: wrongPassphrase(key),
	})
}

// secretsPassphrase asks for the passphrase that opens the secrets,
// which is not a key's and says so.
//
// Its own dialog rather than the key one with a different path in it:
// what is being asked for here is the way back into the vault, and a
// box headed with a key file would have the user typing the wrong
// thing with nothing on screen to say so.
func (u *askUser) secretsPassphrase(ctx context.Context, wrong int) (string, error) {
	s := secret{
		title:  dlgUnlockSecrets,
		lines:  []string{secretsPassphraseAsks},
		labels: []string{fldPassphrase},
		masked: []bool{true},
		accept: btnUnlock,
	}
	if wrong > 0 {
		s.trouble = errWrongPassphrase
	}
	return u.secret(ctx, s)
}

// secretsPassphraseAsks says which passphrase is wanted, because the
// window asks for two kinds and only the wording tells them apart.
const secretsPassphraseAsks = "The passphrase that opens the secrets."

// wrongPassphrase is what the dialog says about the answer before it,
// and nil the first time a key is asked about.
func wrongPassphrase(key remote.LockedKey) error {
	if key.Wrong == 0 {
		return nil
	}
	return errWrongPassphrase
}

// errWrongPassphrase is what the dialog says when the last passphrase
// did not open the key.
var errWrongPassphrase = errors.New("Invalid passphrase")

// errWrongPassword is the same for an account password the server
// refused.
var errWrongPassword = errors.New("Invalid password")

// Password asks for the account password.
//
// A password the server refused comes back here the way a passphrase
// does, and the dialog says so rather than opening again with no word of
// why. Nothing is counted: the server decides when it has had enough,
// and that arrives as a connection that failed.
func (u *askUser) Password(ctx context.Context, user, host string) (string, error) {
	s := secret{
		title:  "Password",
		lines:  []string{user + "@" + host},
		labels: []string{"Password"},
		masked: []bool{true},
		accept: "Sign in",
	}
	if u.askedPassword {
		s.trouble = errWrongPassword
	}
	u.askedPassword = true
	return u.secret(ctx, s)
}

// Question asks whatever the server decided to ask, which is usually a
// one-time code.
func (u *askUser) Question(ctx context.Context, q remote.Question) ([]string, error) {
	// Ours first and always. Everything under it is the server's own
	// wording: a server that chose "Unlock a private key" and a
	// plausible key path would otherwise produce a dialog the user
	// cannot tell from the local one, and be handed the passphrase to
	// their private key.
	// And through serve.Plain, because a dialog draws what it is given:
	// the server's wording must not carry escape sequences into it.
	lines := []string{q.User + "@" + q.Host + " asks:"}
	if q.Name != "" {
		lines = append(lines, "", serve.Plain(q.Name))
	}
	if q.Instruction != "" {
		lines = append(lines, "", serve.Plain(q.Instruction))
	}
	const title = "Authentication"
	masked := make([]bool, len(q.Prompts))
	for i := range q.Prompts {
		// Echo says the answer may be shown as it is typed. Anything the
		// server did not mark that way is a secret.
		masked[i] = i >= len(q.Echo) || !q.Echo[i]
	}
	labels := make([]string, len(q.Prompts))
	for i, prompt := range q.Prompts {
		labels[i] = serve.Plain(prompt)
	}
	return u.ask(ctx, u.form(secret{
		title:  title,
		lines:  lines,
		labels: labels,
		masked: masked,
		accept: "OK",
	}))
}

// TrustHostKey shows a host key that is not in known_hosts and waits for
// a decision.
//
// The fingerprint is spelled the way ssh and ssh-keygen spell it, so the
// user can compare it with whatever they were sent. There is nothing
// else they could check it against.
func (u *askUser) TrustHostKey(ctx context.Context, key remote.HostKey) (bool, error) {
	lines := []string{
		fmt.Sprintf("%s is not in known_hosts.", key.Addr),
		"",
		key.Type() + "  " + key.Fingerprint(),
		"",
		"Verify the fingerprint before connecting.",
	}
	_, err := u.ask(ctx, func(reply func([]string, error)) ui.Widget {
		f := u.app.newConfirm(dlgUnknownHostKey, lines)
		f.AddButton(ui.Button{Title: btnConnect, Do: func() error {
			reply(nil, nil)
			return nil
		}})
		f.AddButton(ui.Button{Title: btnCancel, Do: func() error {
			reply(nil, errDismissed)
			return nil
		}})
		// Opens on Cancel: this is the one question where saying yes by
		// reflex is the answer that cannot be taken back.
		f.FocusButton(1)
		return f
	})
	switch {
	case errors.Is(err, errDismissed):
		// Saying no is an answer, not a failure: the connection stops
		// and the user is not told they did something wrong.
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// secret describes a form asking for one or more secrets.
type secret struct {
	title  string
	lines  []string
	labels []string
	masked []bool
	accept string

	// trouble is what went wrong with the answer before, shown the way a
	// dialog shows a failed attempt. Nil when this is the first time of
	// asking.
	trouble error
}

// secret shows a form asking for secrets and returns what was typed.
func (u *askUser) secret(ctx context.Context, s secret) (string, error) {
	answers, err := u.ask(ctx, u.form(s))
	if err != nil {
		return "", err
	}
	if len(answers) == 0 {
		return "", errDismissed
	}
	return answers[0], nil
}

// form builds the dialog a secret describes.
func (u *askUser) form(s secret) func(reply func([]string, error)) ui.Widget {
	return func(reply func([]string, error)) ui.Widget {
		f := u.app.newForm(s.title)
		f.Lines = s.lines
		fields := make([]*ui.Field, len(s.labels))
		for i, label := range s.labels {
			mask := rune(0)
			if i < len(s.masked) && s.masked[i] {
				mask = '*'
			}
			fields[i] = f.AddField(label, u.app.newField("", mask))
		}
		f.AddButton(ui.Button{Title: s.accept, Do: func() error {
			values := make([]string, len(fields))
			for i, fld := range fields {
				values[i] = fld.Text()
			}
			reply(values, nil)
			return nil
		}})
		f.AddButton(ui.Button{Title: btnCancel, Do: func() error {
			reply(nil, errDismissed)
			return nil
		}})
		// After the buttons, because setting it lays the form out again
		// and a box whose buttons are not on it yet is not the shape it
		// will be. A press clears it: the next attempt is not the one
		// that failed.
		f.SetError(s.trouble)
		return f
	}
}

// ask shows a dialog and waits for it.
//
// The reply handed to build may be called once. A dialog that goes
// without one having been called answers for itself, so clicking away or
// pressing Escape is a refusal rather than a goroutine waiting for ever.
func (u *askUser) ask(ctx context.Context, build func(reply func([]string, error)) ui.Widget) ([]string, error) {
	if err := ctx.Err(); err != nil {
		// Given up on already. A handshake that was walked away from
		// goes on running, and a dialog from it would take the screen
		// for a connection nobody is waiting for any more.
		return nil, err
	}
	type answer struct {
		values []string
		err    error
	}
	ch := make(chan answer, 1)

	// Written by the closure that opens the dialog and read by the one
	// that takes it away. Both run on the drawing goroutine, in the
	// order they were posted, so nothing else has to guard it.
	var dismiss func()

	u.app.pump.post(func() {
		answered := false
		reply := func(values []string, err error) {
			answered = true
			// Buffered and only ever sent once, so a dialog that somehow
			// replies twice cannot block the drawing goroutine.
			select {
			case ch <- answer{values: values, err: err}:
			default:
			}
		}
		w := build(reply)
		dismiss = u.app.showModal(w, func() {
			if !answered {
				reply(nil, errDismissed)
			}
		})
		if f, ok := w.(*ui.Form); ok {
			f.SetClose(dismiss)
		}
	})

	select {
	case a := <-ch:
		return a.values, a.err
	case <-ctx.Done():
		// The window is closing, or the connection was given up on.
		// Whatever is on screen has to go with it.
		u.app.pump.post(func() {
			if dismiss != nil {
				dismiss()
			}
		})
		return nil, ctx.Err()
	}
}

// Notice shows what a server said and returns at once.
//
// A server that signs people in through a browser sends the link this
// way: it says where to go, holds the handshake open, and lets it
// through once the user has been. So nothing here waits for an answer.
// Waiting would hold up the very handshake the user is being asked to
// unblock.
//
// The dialog goes when the connection does, whichever way that turns
// out: made, failed, or given up on.
func (u *askUser) Notice(ctx context.Context, n remote.Notice) {
	if ctx.Err() != nil {
		// Given up on already. A handshake that was walked away from
		// goes on running, and a dialog from it would arrive for a
		// connection nobody is waiting for any more.
		return
	}
	// Into the pane first, where it stays: whole, on lines of its own,
	// and there to select and copy. A dialog is read once and then
	// dismissed, and a sign-in link the user dismissed is gone.
	if u.log != nil {
		for _, line := range []string{n.Name, n.Instruction, n.Text} {
			u.log.Quote(n.User+"@"+n.Host, line)
		}
	}

	// Ours first and always. Everything under it is the server's own
	// wording, and a message the user cannot tell from the window's own
	// could send them somewhere of the server's choosing.
	lines := []string{n.User + "@" + n.Host + ":"}
	// The server's own wording, kept on its own so Copy hands over what
	// the server said and not the line naming it.
	//
	// Through serve.Plain, the same as what is drawn. Copy then puts on
	// the clipboard exactly what the user read, and the link the button
	// offers is one found in text an escape sequence cannot have shaped.
	var said []string
	for _, line := range []string{n.Name, n.Instruction, n.Text} {
		if strings.TrimSpace(line) == "" {
			continue
		}
		plain := serve.Plain(line)
		lines = append(lines, "", plain)
		said = append(said, plain)
	}
	lines = append(lines, "", "Continues automatically when you are done.")

	var dismiss func()
	u.app.pump.post(func() {
		f := u.app.newConfirm(dlgWaitingForServer, lines)
		// This message usually carries a sign-in link, and a link that
		// cannot be copied is a link nobody can follow.
		f.Copyable = strings.Join(said, "\n")
		// The one link, when there is exactly one. A sign-in message is
		// usually a link and a code, and Copy still covers the code;
		// several links give nothing to guess between.
		closeAt := 1
		if link, one := onlyLink(said); one {
			closeAt = 2
			f.AddButton(ui.Button{Title: btnOpenLink, Keep: true, Do: func() error {
				if err := openInBrowser(link); err != nil {
					// Not returned: this button keeps the dialog open,
					// and the reason belongs in front of whoever pressed
					// it rather than under a message from the server.
					u.app.pump.post(func() {
						u.app.reportError("Could not open the link", err)
					})
				}
				return nil
			}})
		}
		f.AddButton(ui.Button{Title: btnCopy, Keep: true, Do: func() error {
			u.app.clip.set(f.Copyable)
			return nil
		}})
		// Close leaves the handshake open: the server is still waiting,
		// and the dialog is only what said so.
		f.AddButton(ui.Button{Title: btnClose})
		if u.stop != nil {
			f.AddButton(ui.Button{Title: btnCancel, Do: func() error {
				u.stop()
				return nil
			}})
		}
		// Opens on Close, which is the one that changes nothing. This
		// dialog arrives unasked for, in the middle of a handshake and
		// possibly while the user is typing somewhere else: a stray
		// Enter must not open a browser at an address a server chose.
		f.FocusButton(closeAt)
		dismiss = u.app.showForm(f, nil)
	})

	// Taken down when the connection is settled, on the goroutine that
	// draws. Nothing waits here: the handshake has to go back to the
	// server before the user can get anywhere.
	go func() {
		<-ctx.Done()
		u.app.pump.post(func() {
			if dismiss != nil {
				dismiss()
			}
		})
	}()
}

// linkSchemes are the only two a server's message may offer a button
// for.
//
// Not the rest of what openInBrowser would take. This text came from a
// server, and mailto, ftp or a scheme some local handler is registered
// for must not be one press away from whatever it decided to send.
var linkSchemes = []string{"http://", "https://"}

// linkLeading and linkTrailing are punctuation a link is written next to
// rather than part of it: a sentence that ends in one, or a link inside
// brackets or quotes, which has an opening character as well as a
// closing one.
const (
	linkLeading  = `(<"'`
	linkTrailing = `.,)>"'`
)

// onlyLink is the one web address in the server's lines.
//
// It reports false when there is none and when there is more than one:
// a message with two links gives nothing to choose between, and a button
// that guessed would send the user to an address they did not pick. The
// same address written twice is still one address.
func onlyLink(lines []string) (string, bool) {
	var found string
	for _, line := range lines {
		for _, word := range strings.Fields(line) {
			at, ok := linkIn(word)
			if !ok {
				continue
			}
			if found != "" && found != at {
				return "", false
			}
			found = at
		}
	}
	return found, found != ""
}

// linkIn is the web address a word holds, trimmed of the punctuation it
// was written beside.
func linkIn(word string) (string, bool) {
	word = strings.TrimLeft(word, linkLeading)
	lower := strings.ToLower(word)
	var is bool
	for _, scheme := range linkSchemes {
		is = is || strings.HasPrefix(lower, scheme)
	}
	if !is {
		return "", false
	}
	at := strings.TrimRight(word, linkTrailing)
	// Everything after the scheme was trimmed away, so there is no
	// address here to open.
	for _, scheme := range linkSchemes {
		if strings.EqualFold(at, scheme) {
			return "", false
		}
	}
	// The same check the window makes of a link in a pane: a line break
	// would end one command line and start another.
	if linkIsOpenable(at) != nil {
		return "", false
	}
	return at, true
}
