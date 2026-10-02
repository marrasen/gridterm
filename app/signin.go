package app

import (
	"context"
	"strings"

	"github.com/marrasen/kakel/links"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/serve"
)

// A server that signs in through a browser says where to go, and waits.
// The window shows it in the connection's log, where it stays to be
// copied, and in a question that offers the one link to open and the
// words to copy, and goes by itself once the connection is made or
// fails. It opens on Close, so a stray Enter opens nothing a server
// chose.

// AskAction is a button of a question that does something and leaves
// the question open, such as Open Link.
type AskAction struct {
	ID     uint64
	Action string
}

// Notice implements [remote.Ask].
func (q asker) Notice(ctx context.Context, n remote.Notice) {
	if ctx.Err() != nil {
		// Given up on already.
		return
	}
	// The server's own words, plain, under a line in the window's own
	// words naming whose they are.
	var said []string
	for _, line := range []string{n.Name, n.Instruction, n.Text} {
		if strings.TrimSpace(line) != "" {
			said = append(said, serve.Plain(line))
		}
	}
	text := n.User + "@" + n.Host + ":\n\n" + strings.Join(said, "\n\n") + "\n\nContinues by itself when you are done."
	ask := Ask{Title: "Waiting for server", Text: text, Yes: "Close", No: "Cancel", Copy: strings.Join(said, "\n")}
	if link, one := links.Only(said); one {
		ask.Link = link
		ask.Actions = append(ask.Actions, "Open Link")
	}
	ask.Actions = append(ask.Actions, "Copy")
	go func() {
		q.a.events <- func() {
			for _, line := range said {
				logLine(q.a.account(q.name), "", n.User+"@"+n.Host+" says: "+line)
			}
			q.a.askThen(ctx, ask, func(ans AskAnswered) {
				if !ans.Yes {
					// Cancel gives the connection up; Close leaves the
					// server waiting.
					q.a.giveUp(q.name)
				}
			})
		}
	}()
}

// askThen shows a question and carries on without waiting: then hears
// the answer, and the question goes when ctx ends first.
func (a *app) askThen(ctx context.Context, q Ask, then func(AskAnswered)) {
	q.ID = a.askIDs.Add(1)
	reply := make(chan AskAnswered, 1)
	a.replies[q.ID] = reply
	a.pose(q)
	go func() {
		select {
		case ans := <-reply:
			a.events <- func() { then(ans) }
		case <-ctx.Done():
			a.events <- func() { a.dropAsk(q.ID) }
		}
	}()
}

// askAction does what a question's button that keeps it open asks.
func (a *app) askAction(in AskAction) error {
	for _, q := range a.st.Asks {
		if q.ID == in.ID && in.Action == "Open Link" && q.Link != "" {
			return openInBrowser(q.Link)
		}
	}
	return nil
}
