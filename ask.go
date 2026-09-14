package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/marrasen/gridterm/remote"
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
type askUser struct{ app *app }

// Passphrase asks for the passphrase of a private key file.
func (u *askUser) Passphrase(ctx context.Context, keyfile string) (string, error) {
	return u.secret(ctx, secret{
		title:  "Unlock a private key",
		lines:  []string{keyfile},
		labels: []string{"Passphrase"},
		masked: []bool{true},
		accept: "Unlock",
	})
}

// Password asks for the account password.
func (u *askUser) Password(ctx context.Context, user, host string) (string, error) {
	return u.secret(ctx, secret{
		title:  "Password",
		lines:  []string{user + "@" + host},
		labels: []string{"Password"},
		masked: []bool{true},
		accept: "Sign in",
	})
}

// Question asks whatever the server decided to ask, which is usually a
// one-time code.
func (u *askUser) Question(ctx context.Context, q remote.Question) ([]string, error) {
	// Ours first and always. Everything under it is the server's own
	// wording: a server that chose "Unlock a private key" and a
	// plausible key path would otherwise produce a dialog the user
	// cannot tell from the local one, and be handed the passphrase to
	// their private key.
	lines := []string{q.User + "@" + q.Host + " is asking:"}
	if q.Name != "" {
		lines = append(lines, "", q.Name)
	}
	if q.Instruction != "" {
		lines = append(lines, "", q.Instruction)
	}
	const title = "The server is asking"
	masked := make([]bool, len(q.Prompts))
	for i := range q.Prompts {
		// Echo says the answer may be shown as it is typed. Anything the
		// server did not mark that way is a secret.
		masked[i] = i >= len(q.Echo) || !q.Echo[i]
	}
	return u.ask(ctx, u.form(secret{
		title:  title,
		lines:  lines,
		labels: q.Prompts,
		masked: masked,
		accept: "Answer",
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
		"Connect only if that is the fingerprint you expect.",
	}
	_, err := u.ask(ctx, func(reply func([]string, error)) ui.Widget {
		f := u.app.newConfirm("Unknown host key", lines)
		f.AddButton(ui.Button{Title: "Connect", Do: func() error {
			reply(nil, nil)
			return nil
		}})
		f.AddButton(ui.Button{Title: "Cancel", Do: func() error {
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
		f.AddButton(ui.Button{Title: "Cancel", Do: func() error {
			reply(nil, errDismissed)
			return nil
		}})
		return f
	}
}

// ask shows a dialog and waits for it.
//
// The reply handed to build may be called once. A dialog that goes
// without one having been called answers for itself, so clicking away or
// pressing Escape is a refusal rather than a goroutine waiting for ever.
func (u *askUser) ask(ctx context.Context, build func(reply func([]string, error)) ui.Widget) ([]string, error) {
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
