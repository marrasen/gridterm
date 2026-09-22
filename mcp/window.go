package mcp

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/gridterm/agent"
)

// Window is the panes of one gridterm window, as an agent reaches them.
//
// It holds no credentials. A session code arrives in a tool call, says
// which window to reach and opens the panes of one share there, and is
// not kept: what is kept is the connection it opened.
type Window struct {
	mu sync.Mutex
	// reached is one connection per gridterm window, by the port its
	// codes name. A user handing over panes of two windows is handing
	// over two windows, and losing the first the moment the second
	// arrives would be a pane they thought they had given away.
	reached map[int]*agent.Client
}

// NewWindow gives an agent somewhere to use a session code.
func NewWindow() *Window { return &Window{reached: map[int]*agent.Client{}} }

// Use opens the panes a session code names.
func (w *Window) Use(code string) ([]Pane, error) {
	port, err := agent.ReadCode(code)
	if err != nil {
		return nil, err
	}
	conn, err := w.connect(port, code)
	if err != nil {
		return nil, err
	}
	sh, err := conn.Use(code)
	if err != nil {
		return nil, err
	}
	out := make([]Pane, 0, len(sh.Panes))
	for _, p := range sh.Panes {
		out = append(out, asPane(port, p))
	}
	return out, nil
}

// List is the panes this agent has been given, across every window it
// has a code for.
//
// Nothing handed over is an empty list rather than a failure: being
// asked what you have and having nothing is an answer.
func (w *Window) List() ([]Pane, error) {
	var out []Pane
	var errs []error
	for port, conn := range w.connections() {
		panes, err := conn.Panes()
		if err != nil {
			if conn.Gone() {
				// The window has closed.
				w.forget(port, conn)
				continue
			}
			errs = append(errs, err)
			continue
		}
		for _, p := range panes {
			out = append(out, asPane(port, p))
		}
	}
	// A window that could not be asked is said, rather than its panes
	// quietly going missing from the list.
	return out, errors.Join(errs...)
}

// Read is the last lines of a pane, the screen when lines is zero.
func (w *Window) Read(id string, lines int) (Screen, error) {
	conn, at, err := w.paneAt(id)
	if err != nil {
		return Screen{}, err
	}
	look, err := conn.Read(at, lines)
	if err != nil {
		return Screen{}, err
	}
	return asScreen(look), nil
}

// Output is what the last command in a pane printed.
func (w *Window) Output(id string, most int) (Screen, error) {
	conn, at, err := w.paneAt(id)
	if err != nil {
		return Screen{}, err
	}
	look, err := conn.Output(at, most)
	if err != nil {
		return Screen{}, err
	}
	return asScreen(look), nil
}

// Restart starts a pane's program again.
func (w *Window) Restart(id string) (Pane, error) {
	conn, at, err := w.paneAt(id)
	if err != nil {
		return Pane{}, err
	}
	pane, err := conn.Restart(at)
	if err != nil {
		return Pane{}, err
	}
	return asPane(w.portOf(id), pane), nil
}

// Open opens another pane where a pane is.
func (w *Window) Open(id string) (Pane, error) {
	conn, at, err := w.paneAt(id)
	if err != nil {
		return Pane{}, err
	}
	pane, err := conn.Open(at)
	if err != nil {
		return Pane{}, err
	}
	return asPane(w.portOf(id), pane), nil
}

// portOf is the window a pane name belongs to, which is the number in
// front of it. A name that has no number is one paneAt would have turned
// away already.
func (w *Window) portOf(id string) int {
	port, _, _ := strings.Cut(id, "/")
	n, _ := strconv.Atoi(port)
	return n
}

// Secret asks the user to type something into a pane and waits.
func (w *Window) Secret(id, what string, wait time.Duration) (bool, error) {
	conn, at, err := w.paneAt(id)
	if err != nil {
		return false, err
	}
	return conn.Secret(at, what, wait)
}

// Send types into a pane and presses the keys named after it.
func (w *Window) Send(id, text string, keys []string) error {
	conn, at, err := w.paneAt(id)
	if err != nil {
		return err
	}
	return conn.Send(at, text, keys)
}

// Wait watches a pane until something happens or the time runs out.
func (w *Window) Wait(id string, lines int, until Until) (Screen, Ending, error) {
	conn, at, err := w.paneAt(id)
	if err != nil {
		return Screen{}, Ending{}, err
	}
	look, ended, err := conn.Wait(at, lines, agent.Until{
		Contains:  until.Contains,
		SinceKeys: until.SinceKeys,
		QuietMS:   until.QuietMS,
		TimeoutMS: until.TimeoutMS,
	})
	if err != nil {
		return Screen{}, Ending{}, err
	}
	return asScreen(look), Ending{GaveUp: ended.GaveUp, Because: ended.Because}, nil
}

// asScreen turns what a window said about a pane into what an agent is
// told.
func asScreen(look agent.Look) Screen {
	return Screen{
		Screen:    look.Screen,
		Gone:      look.Gone,
		Row:       look.Row,
		Col:       look.Col,
		Alt:       look.Alt,
		All:       look.All,
		Trimmed:   look.Trimmed,
		Pictures:  picturesOf(look.Pictures),
		Note:      look.Note,
		Marks:     look.Marks,
		Running:   look.Running,
		Done:      look.Done,
		Status:    look.Status,
		HasStatus: look.HasStatus,
		Back:      look.Back,
		Watching:  look.Watching,
		Yours:     look.Yours,
	}
}

// Close lets go of every window.
func (w *Window) Close() error {
	w.mu.Lock()
	reached := w.reached
	w.reached = map[int]*agent.Client{}
	w.mu.Unlock()

	var errs []error
	for _, conn := range reached {
		errs = append(errs, conn.Close())
	}
	return errors.Join(errs...)
}

// connect is the connection to one window, opening it the first time.
//
// One that has gone is let go of and dialled again. A fresh code for a
// window whose connection broke is the user handing the pane over
// again, and answering it with the old socket's failure for ever would
// mean restarting this process to use it.
func (w *Window) connect(port int, code string) (*agent.Client, error) {
	w.mu.Lock()
	have := w.reached[port]
	if have != nil && have.Gone() {
		delete(w.reached, port)
		have = nil
	}
	w.mu.Unlock()
	if have != nil {
		return have, nil
	}
	conn, err := agent.Dial(code)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	// Another code for the same window may have opened one while this
	// was dialling. The first one kept, so an id already handed out
	// goes on naming something.
	if have := w.reached[port]; have != nil {
		return have, conn.Close()
	}
	w.reached[port] = conn
	return conn, nil
}

// connections is every window reached, by port.
func (w *Window) connections() map[int]*agent.Client {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[int]*agent.Client, len(w.reached))
	for port, conn := range w.reached {
		if conn.Gone() {
			// A window that has closed cannot be asked again.
			delete(w.reached, port)
			continue
		}
		out[port] = conn
	}
	return out
}

// forget drops a window this agent had reached, and only while it is
// still the one reached on that port.
//
// A fresh code for the same window dials again and takes the port back,
// on a goroutine of its own. Deleting by port alone would throw that
// connection away instead.
func (w *Window) forget(port int, conn *agent.Client) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.reached[port] == conn {
		delete(w.reached, port)
	}
}

// paneAt finds the window a pane name belongs to, and what that window
// calls it.
func (w *Window) paneAt(id string) (*agent.Client, string, error) {
	port, at, ok := strings.Cut(id, "/")
	if ok {
		if n, err := strconv.Atoi(port); err == nil {
			w.mu.Lock()
			conn := w.reached[n]
			w.mu.Unlock()
			if conn != nil {
				return conn, at, nil
			}
		}
	}
	if len(w.connections()) == 0 {
		return nil, "", errors.New(
			"you have no panes: ask the user for a session code and use it with" +
				" use_session_code")
	}
	return nil, "", fmt.Errorf("%q is not a pane you have been handed", id)
}

// asPane turns what a window said into what an agent is told, naming
// the window it is on so two windows cannot name the same pane.
func asPane(port int, p agent.Pane) Pane {
	return Pane{
		ID:    strconv.Itoa(port) + "/" + p.ID,
		Label: p.Label,
		Cols:  p.Cols,
		Rows:  p.Rows,
		Ended: p.Ended,
		May: May{
			Restart:  p.May.Restart,
			OpenMore: p.May.OpenMore,
			ReadOnly: p.May.ReadOnly,
			ReadBack: p.May.ReadBack,
		},
	}
}

// picturesOf is what the wire said about the pictures on a screen.
func picturesOf(on []agent.Picture) []Picture {
	if len(on) == 0 {
		return nil
	}
	out := make([]Picture, 0, len(on))
	for _, p := range on {
		out = append(out, Picture{
			Top: p.Top, Rows: p.Rows, Cols: p.Cols,
			Width: p.Width, Height: p.Height, Wire: p.Wire,
		})
	}
	return out
}
