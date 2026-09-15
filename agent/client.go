package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Client is an agent's end of the connection to a window.
//
// One at a time: a client is written to and read from by whatever is
// asking, and an answer belongs to the request before it. The lock is
// what makes that true when two questions are asked at once.
type Client struct {
	conn net.Conn
	in   *bufio.Reader
	out  *json.Encoder

	mu     sync.Mutex
	closed bool
}

// Dial reaches the window a code names.
//
// The code says which window: two on one machine hand out codes of the
// same shape, and the port in the code is what tells them apart.
func Dial(code string) (*Client, error) {
	port, err := ReadCode(code)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("agent: no gridterm window is listening on port %d: %w", port, err)
	}
	return &Client{conn: conn, in: bufio.NewReaderSize(conn, 4096), out: json.NewEncoder(conn)}, nil
}

// Use gives the window a code and gets back the pane it names.
func (c *Client) Use(code string) (Pane, error) {
	got, err := c.say(ask{Do: "use", Code: code})
	if err != nil {
		return Pane{}, err
	}
	if got.Pane == nil {
		return Pane{}, errors.New("agent: the window named no pane")
	}
	return *got.Pane, nil
}

// Panes is what this agent has been handed.
func (c *Client) Panes() ([]Pane, error) {
	got, err := c.say(ask{Do: "panes"})
	if err != nil {
		return nil, err
	}
	return got.Panes, nil
}

// Read is what is on a pane now.
func (c *Client) Read(id string) (Look, error) {
	got, err := c.say(ask{Do: "read", Pane: id})
	if err != nil {
		return Look{}, err
	}
	if got.Look == nil {
		return Look{}, errors.New("agent: the window sent no screen")
	}
	return *got.Look, nil
}

// Send types into a pane.
func (c *Client) Send(id, text string) error {
	_, err := c.say(ask{Do: "send", Pane: id, Text: text})
	return err
}

// Wait watches a pane until it says what was asked for, goes quiet, or
// the time runs out. It reports whether the time ran out.
func (c *Client) Wait(id string, until Until) (Look, bool, error) {
	got, err := c.say(ask{Do: "wait", Pane: id, Until: wait(until)})
	if err != nil {
		return Look{}, false, err
	}
	if got.Look == nil {
		return Look{}, false, errors.New("agent: the window sent no screen")
	}
	return *got.Look, got.Waited, nil
}

// Until says what a wait is waiting for. Whichever happens first.
type Until struct {
	// Contains ends the wait as soon as the screen holds this text.
	Contains string

	// QuietMS ends it once the pane has said nothing for this long.
	// Zero asks the window for its own idea of long enough.
	QuietMS int

	// TimeoutMS gives up after this long and says what is on the screen
	// anyway. Zero asks the window for its own.
	TimeoutMS int
}

// Close hangs up. The panes go on running, and the user still has them.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if err := c.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("agent: close: %w", err)
	}
	return nil
}

// say asks one thing and waits for the answer to it.
func (c *Client) say(want ask) (said, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return said{}, errors.New("agent: that window has been let go of")
	}
	if err := c.out.Encode(want); err != nil {
		return said{}, fmt.Errorf("agent: ask the window: %w", err)
	}
	line, err := readLine(c.in)
	if err != nil {
		return said{}, fmt.Errorf("agent: the window stopped answering: %w", err)
	}
	var got said
	if err := json.Unmarshal(line, &got); err != nil {
		return said{}, fmt.Errorf("agent: the window said something unreadable: %w", err)
	}
	if got.Error != "" {
		return said{}, errors.New(got.Error)
	}
	return got, nil
}
