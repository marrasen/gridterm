package view

import (
	"strconv"
	"strings"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/words"
)

// servingDialog serves the window, or, while it is served, says where
// and to whom.
func (w *Window) servingDialog(s app.Serving, u *gunim.UI) {
	if s.On {
		w.servedDialog(s, u)
		return
	}
	form := widget.NewForm().Add("", widget.NewLabel("A connected window can open shells here, use the ones running, and read and write files as you."))
	switch {
	case s.Problem != "":
		form.Add("", widget.NewLabel(words.UpperFirst(s.Problem)+"."))
	case len(s.Allowed) == 0:
		form.Add("", widget.NewLabel("No key may connect yet. Put the public key of the machine you will connect from in "+s.AllowedAt+"."))
	default:
		form.Add("Allowed", widget.NewLabel(strings.Join(s.Allowed, "\n")))
	}
	port := widget.NewTextField()
	port.SetText(strconv.Itoa(s.Port))
	port.Placeholder = "0 picks a free port"
	where := widget.NewDropdown("This machine only", "All networks")
	where.Label = "Listen on"
	if s.Anywhere {
		where.Selected = 1
	}
	form.Add("Port", port).Add("Listen on", where)
	d := widget.NewDialog("Serve This Window")
	d.Body = form
	d.SetButtons("Serve", "Cancel")
	d.Check = func() string {
		n, err := strconv.Atoi(strings.TrimSpace(port.Text()))
		switch {
		case err != nil || n < 0 || n > 65535:
			return "The port is a number from 0 to 65535."
		case len(s.Allowed) == 0:
			return "Add a key that may connect first."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent {
		// Shown once it is served: the host key, to check from the
		// other end. Nothing, if serving did not start.
		w.servingAsked, w.servingTries = true, s.Tries
		return app.StartServing{Port: port.Text(), Anywhere: where.Selected == 1}
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// servedDialog says where the window is served and who is connected.
func (w *Window) servedDialog(s app.Serving, u *gunim.UI) {
	form := widget.NewForm().
		Add("Address", widget.NewLabel(s.Addr)).
		Add("Host key", widget.NewLabel(s.Fingerprint))
	who := widget.NewLabel(connectedSays(s))
	form.Add("Connected", who)
	d := widget.NewDialog("Serving This Window")
	d.Body = form
	d.SetButtons("Done", "")
	d.AddButton("Disconnect All", func() gunim.Intent { return app.DisconnectClients{} })
	d.AddButton("Stop Serving", func() gunim.Intent { return app.StopServing{} })
	d.Accept, d.Dismiss = app.DialogClosed{}, app.DialogClosed{}
	w.served = &servedShown{d: d, who: who}
	w.openDialog(d, u)
}

// connectedSays is who is connected to the window served, one a line.
func connectedSays(s app.Serving) string {
	if len(s.Clients) == 0 {
		return "Nobody yet."
	}
	var lines []string
	for _, c := range s.Clients {
		lines = append(lines, c.Name+", from "+c.From)
		for _, t := range s.Tunnels {
			if t.Client == c.Name && t.From == c.From {
				lines = append(lines, "    a tunnel, "+t.Label+", on "+t.On)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// servedShown is the dialog saying the window is served, while it is
// open, and its line of who is connected.
type servedShown struct {
	d   *widget.Dialog
	who *widget.Label
}

// showServed keeps the dialog saying the window is served up to date
// as windows connect and go, closes it once serving stops, and opens
// it once serving asked for has started.
func (w *Window) showServed(s app.Serving, u *gunim.UI) {
	if sh := w.served; sh != nil {
		switch {
		case u.Presence(sh.d) == gunim.Exiting || w.dialog != sh.d:
			w.served = nil
		case !s.On:
			w.served = nil
			sh.d.Close(u)
		default:
			sh.who.SetText(connectedSays(s))
			u.Invalidate()
		}
	}
	// The one asked for, once it has been tried: shown if it started,
	// once the dialog that asked has gone.
	if w.servingAsked && s.Tries > w.servingTries {
		if !s.On {
			w.servingAsked = false
		} else if w.dialog == nil || u.Presence(w.dialog) == gunim.Exiting {
			w.servingAsked = false
			w.servedDialog(s, u)
		}
	}
}

// connectWindowDialog asks for a served window to connect to.
func (w *Window) connectWindowDialog(u *gunim.UI) {
	addr, key := widget.NewTextField(), widget.NewTextField()
	addr.Placeholder = "host, or host:" + strconv.Itoa(remote.ServePort)
	key.Placeholder = "the usual keys in ~/.ssh"
	d := widget.NewDialog("Connect to Window")
	d.Body = widget.NewForm().
		Add("", widget.NewLabel("The other window must be served, with this machine's public key in its authorized keys.")).
		Add("Address", addr).Add("Key file", key)
	d.SetButtons("Connect", "Cancel")
	d.Check = func() string {
		if strings.TrimSpace(addr.Text()) == "" {
			return "Type the other window's address."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent { return app.ConnectWindow{Addr: addr.Text(), KeyFile: key.Text()} }
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}
