package mcp

import (
	"errors"
	"sync"

	"github.com/marrasen/gridterm/agent"
)

// Window is the panes of one gridterm window, as an agent reaches them.
//
// It holds no credentials. A session code arrives in a tool call, says
// which window to reach and opens one pane there, and is not kept: what
// is kept is the connection it opened.
type Window struct {
	mu   sync.Mutex
	conn *agent.Client
	// port says which window the connection is to, so a code for
	// another one opens another connection rather than being offered to
	// the wrong window.
	port int
}

// NewWindow gives an agent somewhere to use a session code.
func NewWindow() *Window { return &Window{} }

// Use opens the pane a session code names.
//
// The first code opens the connection. A second one is used on the same
// connection when it names the same window, and opens a second one when
// it does not: a user handing over panes of two windows is handing over
// two windows.
func (w *Window) Use(code string) (Pane, error) {
	port, err := agent.ReadCode(code)
	if err != nil {
		return Pane{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil || w.port != port {
		if w.conn != nil {
			if err := w.conn.Close(); err != nil {
				return Pane{}, err
			}
			w.conn = nil
		}
		conn, err := agent.Dial(code)
		if err != nil {
			return Pane{}, err
		}
		w.conn, w.port = conn, port
	}
	pane, err := w.conn.Use(code)
	if err != nil {
		return Pane{}, err
	}
	return asPane(pane), nil
}

// List is the panes this agent has been given.
//
// Nothing handed over is an empty list rather than a failure: being
// asked what you have and having nothing is an answer.
func (w *Window) List() ([]Pane, error) {
	w.mu.Lock()
	conn := w.conn
	w.mu.Unlock()
	if conn == nil {
		return nil, nil
	}
	panes, err := conn.Panes()
	if err != nil {
		return nil, err
	}
	out := make([]Pane, 0, len(panes))
	for _, p := range panes {
		out = append(out, asPane(p))
	}
	return out, nil
}

// Read is what is on a pane now.
func (w *Window) Read(id string) (Screen, error) {
	conn, err := w.reach()
	if err != nil {
		return Screen{}, err
	}
	look, err := conn.Read(id)
	if err != nil {
		return Screen{}, err
	}
	return Screen{Screen: look.Screen, Gone: look.Gone}, nil
}

// Send types into a pane.
func (w *Window) Send(id, text string) error {
	conn, err := w.reach()
	if err != nil {
		return err
	}
	return conn.Send(id, text)
}

// Wait watches a pane until something happens or the time runs out.
func (w *Window) Wait(id string, until Until) (Screen, bool, error) {
	conn, err := w.reach()
	if err != nil {
		return Screen{}, false, err
	}
	look, gaveUp, err := conn.Wait(id, agent.Until{
		Contains:  until.Contains,
		QuietMS:   until.QuietMS,
		TimeoutMS: until.TimeoutMS,
	})
	if err != nil {
		return Screen{}, false, err
	}
	return Screen{Screen: look.Screen, Gone: look.Gone}, gaveUp, nil
}

// Close lets go of the window.
func (w *Window) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn == nil {
		return nil
	}
	conn := w.conn
	w.conn = nil
	return conn.Close()
}

// reach is the connection, or says there is none yet.
func (w *Window) reach() (*agent.Client, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn == nil {
		return nil, errors.New(
			"the user has not handed you a pane yet: ask them for a session code" +
				" and use it with use_session_code")
	}
	return w.conn, nil
}

// asPane turns what the window said into what an agent is told.
func asPane(p agent.Pane) Pane {
	return Pane{ID: p.ID, Label: p.Label, Cols: p.Cols, Rows: p.Rows}
}
