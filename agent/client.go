package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// ErrGone says the window is not answering any more: it has closed, or
// the connection to it has.
var ErrGone = errors.New("agent: that window is no longer answering")

// Client is an agent's end of the connection to a window.
//
// One at a time: a client is written to and read from by whatever is
// asking, and an answer belongs to the request before it. The lock is
// what makes that true when two questions are asked at once.
type Client struct {
	conn net.Conn
	in   *bufio.Reader
	out  *json.Encoder

	// asking is held for the whole of one question and its answer, so
	// an answer belongs to the question before it.
	asking sync.Mutex

	// patience is how long an ordinary question has to be answered.
	patience time.Duration

	// shut is its own lock, and a small one. Closing must not wait for
	// a question to be answered: a wait can be minutes long, and the
	// thing that wants to close is often what would end it.
	shut   sync.Mutex
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
	c := &Client{
		conn:     conn,
		in:       bufio.NewReaderSize(conn, 4096),
		out:      json.NewEncoder(conn),
		patience: promptly,
	}
	if _, err := c.say(ask{Do: "hello", Protocol: hello}); err != nil {
		return nil, errors.Join(
			fmt.Errorf("agent: %s is not a gridterm window this can talk to: %w",
				conn.RemoteAddr(), err),
			c.Close())
	}
	return c, nil
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

// Read is what is on a pane now, as the last lines of it. Zero lines is
// the screen; more than it holds reaches into what has scrolled off.
func (c *Client) Read(id string, lines int) (Look, error) {
	got, err := c.say(ask{Do: "read", Pane: id, Lines: lines})
	if err != nil {
		return Look{}, err
	}
	if got.Look == nil {
		return Look{}, errors.New("agent: the window sent no screen")
	}
	return *got.Look, nil
}

// Send types text into a pane and then presses the named keys. Either
// may be empty, and a name the window does not know is refused with the
// names it has.
func (c *Client) Send(id, text string, keys []string) error {
	_, err := c.say(ask{Do: "send", Pane: id, Text: text, Keys: keys})
	return err
}

// Wait watches a pane until it says what was asked for, goes quiet, or
// the time runs out. It says how the waiting ended. Lines is how much of
// the pane to give back, as for Read.
func (c *Client) Wait(id string, lines int, until Until) (Look, Ending, error) {
	got, err := c.say(ask{Do: "wait", Pane: id, Lines: lines, Until: wait(until)})
	if err != nil {
		return Look{}, Ending{}, err
	}
	if got.Look == nil {
		return Look{}, Ending{}, errors.New("agent: the window sent no screen")
	}
	return *got.Look, Ending{GaveUp: got.Waited, Because: got.Because}, nil
}

// why says which ending this was, keeping what went wrong and what
// closing the connection said along with it.
//
// A window that ran out of time is not the same as one that hung up, and
// the two ask the user for different things: wait, or go and look at the
// window, against it has gone. Both are ErrGone, because the connection
// is finished with either way.
//
// The deadline is asked about as well as the error, because readLine
// reads a timeout as the conversation ending -- which is the right
// answer on the window's side of it and not here.
func why(err, closed error, bound time.Duration, by time.Time) error {
	end := error(ErrGone)
	if errors.Is(err, os.ErrDeadlineExceeded) || !time.Now().Before(by) {
		end = fmt.Errorf("%w: it did not answer within %v", ErrGone, bound)
	}
	if errors.Is(err, errGone) {
		// The conversation ending, which end already says, and says in
		// the words of this side of it.
		err = nil
	}
	return errors.Join(end, err, closed)
}

// promptly is how long a window has to answer an ordinary question.
// Long enough that a window busy drawing is not given up on, short
// enough that an agent gets an answer rather than a hang.
const promptly = 30 * time.Second

// answerWithin is how long this question may take.
//
// A wait is the one question meant to be slow: it takes as long as it
// was asked to take, so it gets that plus the time a window needs to
// notice and reply. Everything else gets promptly.
func (c *Client) answerWithin(want ask) time.Duration {
	// A client built without one -- a zero value, or a test -- still
	// gets a bound, because a deadline of now is a question that fails
	// before it is asked.
	patience := c.patience
	if patience <= 0 {
		patience = promptly
	}
	if want.Do != "wait" {
		return patience
	}
	asked := time.Duration(want.Until.TimeoutMS) * time.Millisecond
	if asked <= 0 || asked > longestWait {
		asked = longestWait
	}
	return asked + patience
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
//
// It does not wait for a question still being answered. Closing the
// connection is what ends one: the read it is parked in fails, and the
// question comes back saying the window stopped answering.
func (c *Client) Close() error {
	c.shut.Lock()
	defer c.shut.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if err := c.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("agent: close: %w", err)
	}
	return nil
}

// isClosed reports whether this has been hung up on.
func (c *Client) isClosed() bool {
	c.shut.Lock()
	defer c.shut.Unlock()
	return c.closed
}

// Gone reports whether this connection is finished with, either because
// it was closed or because the window stopped answering.
func (c *Client) Gone() bool { return c.isClosed() }

// say asks one thing and waits for the answer to it.
//
// A connection that breaks is closed here, so the next question says
// the window has gone rather than handing back whatever the network
// last complained about. Reading that twice is how an agent ends up
// with a socket error where an explanation belongs.
func (c *Client) say(want ask) (said, error) {
	c.asking.Lock()
	defer c.asking.Unlock()
	if c.isClosed() {
		return said{}, ErrGone
	}
	// A window that accepts, greets and then says nothing would park
	// this for ever, and the process running an agent has nothing that
	// could close the connection from the side. The bound covers the
	// question as well as the answer: a window that has stopped reading
	// blocks the write.
	bound := c.answerWithin(want)
	by := time.Now().Add(bound)
	if err := c.conn.SetDeadline(by); err != nil {
		return said{}, errors.Join(ErrGone, err, c.Close())
	}
	if err := c.out.Encode(want); err != nil {
		return said{}, why(err, c.Close(), bound, by)
	}
	// The answer may be a whole scrollback, so it is read against the
	// larger of the two limits.
	line, err := readLine(c.in, longestAnswer)
	if err != nil {
		return said{}, why(err, c.Close(), bound, by)
	}
	if err := c.conn.SetDeadline(time.Time{}); err != nil {
		return said{}, errors.Join(ErrGone, err, c.Close())
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
