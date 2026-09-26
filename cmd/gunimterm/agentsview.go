package main

import (
	"slices"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/settings"
)

// The window's side of sharing panes with an agent.

// shareDialog shows the share: its code, the terminal panes with a box
// for each that is in it, and the agent program the prompt is written
// for. Ticking a box shares that pane at once. With no share yet it
// starts one with the focused pane.
func (w *window) shareDialog(st Share, u *gunim.UI) {
	if st.Code == "" {
		if w.kindOf(w.focused) != kindTerminal {
			w.toasts.Show(widget.Toast{Title: "Share a terminal pane", Body: "Click into the terminal to share, then choose Share with an Agent."}, u)
			return
		}
		u.Send(w, SharePane{Pane: w.focused})
		w.sharing = true
		return
	}
	shared := map[string]bool{}
	for _, p := range st.Panes {
		shared[p.Pane] = true
	}
	host := widget.NewDropdown(agentHostNames()...)
	host.Label = "Agent"
	host.Selected = max(0, slices.Index(agentHostNames(), st.Host))
	hostName := func() string { return agentHostNames()[max(0, min(host.Selected, len(agentHosts)-1))] }
	code := widget.NewLabel(st.Code)
	form := widget.NewForm().
		Add("", widget.NewLabel("An agent with this code can read and type in the ticked panes, and reaches nothing else. Copy Prompt puts the code on the clipboard with how to use it.")).
		Add("Code", code).
		Add("Agent", host)
	for _, p := range w.panes {
		if p.Kind != kindTerminal {
			continue
		}
		label := p.Title
		if p.Machine != "" {
			label += " on " + p.Machine
		}
		box := widget.NewCheckbox(label)
		box.On = shared[p.ID]
		id := p.ID
		box.OnFlip(func(on bool, u *gunim.UI) {
			if on {
				u.Send(w, SharePane{Pane: id})
			} else {
				u.Send(w, UnsharePane{Pane: id})
			}
		})
		form.Add("", box)
	}
	d := widget.NewDialog("Agent Share")
	d.Body = form
	d.SetButtons("Done", "")
	d.AddAction("Copy Prompt", func(u *gunim.UI) { u.Send(w, CopyAgentPrompt{Host: hostName()}) })
	d.AddAction("Setup…", func(u *gunim.UI) { w.setupDialog(hostNamed(hostName()), u) })
	d.AddAction("Write Skill", func(u *gunim.UI) { u.Send(w, WriteSkill{Host: hostName()}) })
	d.AddButton("Stop Sharing", func() gunim.Intent { return StopSharing{} })
	d.Accept, d.Dismiss = DialogClosed{}, DialogClosed{}
	w.openDialog(d, u)
}

// permissionsDialog shows what the agent may do in the focused pane
// beyond reading and typing. Each box takes effect as it is ticked.
func (w *window) permissionsDialog(st Share, u *gunim.UI) {
	i := slices.IndexFunc(st.Panes, func(p SharedPane) bool { return p.Pane == w.focused })
	if i < 0 {
		w.toasts.Show(widget.Toast{Title: "This pane is not shared", Body: "Share it with an agent first."}, u)
		return
	}
	may := st.Panes[i].May
	id := w.focused
	form := widget.NewForm().Add("", widget.NewLabel("The agent can read this pane and type into it. Changes apply at once."))
	for _, b := range []struct {
		label string
		on    *bool
	}{
		{agent.BoxReadOnly, &may.ReadOnly},
		{agent.BoxReadBack, &may.ReadBack},
		{agent.BoxOpenMore, &may.OpenMore},
		{agent.BoxRestart, &may.Restart},
	} {
		box := widget.NewCheckbox(b.label)
		box.On = *b.on
		on := b.on
		box.OnFlip(func(v bool, u *gunim.UI) {
			*on = v
			u.Send(w, SetAgentMay{Pane: id, May: settings.AgentMay(may)})
		})
		form.Add("", box)
	}
	d := widget.NewDialog("Agent Permissions")
	d.Body = form
	d.SetButtons("Done", "")
	d.AddButton("Take Back", func() gunim.Intent { return UnsharePane{Pane: id} })
	d.Accept, d.Dismiss = DialogClosed{}, DialogClosed{}
	w.openDialog(d, u)
}

// setupDialog shows how to add gridterm's MCP server to an agent
// program, and copies it.
func (w *window) setupDialog(host agentHost, u *gunim.UI) {
	what := "Run this, as one command line, then start " + host.called + " again. It only writes the config."
	copyTitle := "Copy Command"
	if host.cmd == "" {
		where := host.configAt
		if where == "" {
			where = "its MCP config"
		}
		what = "Put this in " + where + ", beside any servers already there. Then start " + host.called + " again."
		copyTitle = "Copy Config"
	}
	line := host.setupToCopy(exePath())
	d := widget.NewDialog("Set Up " + host.called)
	d.Body = widget.NewForm().Add("", widget.NewLabel(what)).Add("", widget.NewLabel(line))
	d.SetButtons("Close", "")
	d.AddAction(copyTitle, func(u *gunim.UI) { u.Send(w, CopyAgentSetup{Host: host.name}) })
	d.Accept, d.Dismiss = DialogClosed{}, DialogClosed{}
	w.openDialog(d, u)
}
