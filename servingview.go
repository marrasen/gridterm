package main

import (
	"strconv"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/remote"
)

// servingDialog serves the window, or, while it is served, says where
// and to whom.
func (w *window) servingDialog(s Serving, u *gunim.UI) {
	if s.On {
		w.servedDialog(s, u)
		return
	}
	form := widget.NewForm().Add("", widget.NewLabel("A connected window can open shells here, use the ones running, and read and write files as you."))
	switch {
	case s.Problem != "":
		form.Add("", widget.NewLabel(upperFirst(s.Problem)+"."))
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
	d.OnAccept = func() gunim.Intent { return StartServing{Port: port.Text(), Anywhere: where.Selected == 1} }
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// servedDialog says where the window is served and who is connected.
func (w *window) servedDialog(s Serving, u *gunim.UI) {
	form := widget.NewForm().
		Add("Address", widget.NewLabel(s.Addr)).
		Add("Host key", widget.NewLabel(s.Fingerprint))
	connected := "Nobody yet."
	if len(s.Clients) > 0 {
		var lines []string
		for _, c := range s.Clients {
			lines = append(lines, c.Name+", from "+c.From)
		}
		connected = strings.Join(lines, "\n")
	}
	form.Add("Connected", widget.NewLabel(connected))
	d := widget.NewDialog("Serving This Window")
	d.Body = form
	d.SetButtons("Done", "")
	if len(s.Clients) > 0 {
		d.AddButton("Disconnect All", func() gunim.Intent { return DisconnectClients{} })
	}
	d.AddButton("Stop Serving", func() gunim.Intent { return StopServing{} })
	d.Accept, d.Dismiss = DialogClosed{}, DialogClosed{}
	w.openDialog(d, u)
}

// connectWindowDialog asks for a served window to connect to.
func (w *window) connectWindowDialog(u *gunim.UI) {
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
	d.OnAccept = func() gunim.Intent { return ConnectWindow{Addr: addr.Text(), KeyFile: key.Text()} }
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}
