package main

import (
	"io"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/gridterm/session"
)

// connLog is a session that shows a connection being made and then
// becomes that connection.
//
// A pane opens on it straight away, so there is somewhere to watch from
// while the connecting happens: each step as it is tried, and whatever
// the far end says, including a sign-in link. When the connection is
// made the same pane carries it, and what was written stays above it in
// the scrollback.
//
// The pane cannot tell. It reads a session, and this is one: first its
// own words, then the shell's.
type connLog struct {
	// clock says what time it is, so a test can have one that does not
	// move.
	clock func() time.Time

	// stop is called when the pane is closed before the connection was
	// made, which is how closing the pane gives up on it.
	stop func()

	mu sync.Mutex
	// said is what has been written and not yet read.
	said []byte
	// live is the connection once it has been made, and nil until then.
	live session.Session
	// over says nothing more will happen: it failed, or it was given up
	// on.
	over bool
	// cols and rows are what the pane asked for before there was
	// anything to tell.
	cols, rows int

	// wake carries the fact that there is something new to read.
	// Buffered to one, and read by the pane's reader and nothing else.
	wake chan struct{}

	// settled is closed once there is a connection or there never will
	// be, which is what waiting on the pane waits for.
	settleOnce sync.Once
	settled    chan struct{}

	closeOnce sync.Once
	closed    chan struct{}
}

// newConnLog opens a log for a connection being made. stop is called if
// the pane is closed before the connection is, and may be nil.
func newConnLog(stop func()) *connLog {
	return &connLog{
		clock:   time.Now,
		stop:    stop,
		wake:    make(chan struct{}, 1),
		settled: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

// Say writes a line into the pane, stamped with the time.
//
// Called from whichever goroutine is doing the connecting, which is not
// the one that draws: what it writes is read by the pane's own reader
// like anything else a program says.
func (c *connLog) Say(what string) {
	c.write(c.clock().Format("15:04:05") + "  " + what)
}

// Quote writes what the far end said, marked as its words rather than
// this window's.
//
// Said plainly and in full. A server that signs people in through a
// browser sends a link this way, and a link that is cut off or that
// cannot be copied is a link nobody can use.
func (c *connLog) Quote(who, what string) {
	what = strings.TrimRight(what, "\r\n")
	if strings.TrimSpace(what) == "" {
		return
	}
	c.Say(who + " says:")
	for _, line := range strings.Split(what, "\n") {
		c.write("    " + strings.TrimRight(line, "\r"))
	}
}

// Failed says why the connection was not made. Nothing more is written
// after it, and the pane stays so the reason can be read.
func (c *connLog) Failed(err error) {
	if err == nil {
		return
	}
	c.Say(err.Error())
	c.write("")
	c.write("The connection was not made. This pane is only the record of")
	c.write("it: close it when you have read it.")
	c.end()
}

// GaveUp says the user gave up on the connection.
func (c *connLog) GaveUp() {
	c.Say("given up on")
	c.end()
}

// Became hands the pane over to the connection that was made.
//
// From here the pane reads and writes the real thing, and what was
// written before it stays in the scrollback above.
func (c *connLog) Became(live session.Session) {
	c.mu.Lock()
	if c.over || c.live != nil {
		c.mu.Unlock()
		// Given up on, or already handed over. Whatever arrived is
		// nobody's now, and closing it is all that is left to do with
		// it.
		_ = live.Close()
		return
	}
	c.live = live
	cols, rows := c.cols, c.rows
	c.mu.Unlock()
	c.settle()

	// The size the pane already had. A shell that started at eighty by
	// twenty-four and was told the truth afterwards has drawn its first
	// screen wrong.
	if cols > 0 && rows > 0 {
		if err := live.Resize(cols, rows); err != nil {
			c.Say("could not tell it how big this pane is: " + err.Error())
		}
	}
	c.signal()
}

// write adds one line, ending it the way a terminal expects.
func (c *connLog) write(line string) {
	c.mu.Lock()
	if !c.over {
		c.said = append(c.said, []byte(line+"\r\n")...)
	}
	c.mu.Unlock()
	c.signal()
}

// end says nothing more will be written.
func (c *connLog) end() {
	c.mu.Lock()
	c.over = true
	c.mu.Unlock()
	c.settle()
	c.signal()
}

// settle says there either is a connection now or there never will be.
func (c *connLog) settle() { c.settleOnce.Do(func() { close(c.settled) }) }

// signal wakes the reader, if it is waiting.
func (c *connLog) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Read gives what has been written, and then what the connection says.
func (c *connLog) Read(p []byte) (int, error) {
	for {
		c.mu.Lock()
		if len(c.said) > 0 {
			n := copy(p, c.said)
			c.said = c.said[n:]
			c.mu.Unlock()
			return n, nil
		}
		live, over := c.live, c.over
		c.mu.Unlock()

		// Everything this window had to say has been read, so the rest
		// of the pane is the connection itself.
		if live != nil {
			return live.Read(p)
		}
		if over {
			return 0, io.EOF
		}
		select {
		case <-c.wake:
		case <-c.closed:
			return 0, io.EOF
		}
	}
}

// Write types into the connection, once there is one.
//
// Before that there is nothing to type into. What is on the screen is
// this window's account of what it is doing, and a keystroke has
// nowhere to go: it is dropped rather than kept, because a line typed
// now and delivered to a shell a minute later is a line nobody meant to
// run.
func (c *connLog) Write(p []byte) (int, error) {
	c.mu.Lock()
	live := c.live
	c.mu.Unlock()
	if live != nil {
		return live.Write(p)
	}
	return len(p), nil
}

// Resize remembers how big the pane is, and tells the connection once
// there is one.
func (c *connLog) Resize(cols, rows int) error {
	c.mu.Lock()
	c.cols, c.rows = cols, rows
	live := c.live
	c.mu.Unlock()
	if live != nil {
		return live.Resize(cols, rows)
	}
	return nil
}

// Wait blocks until the connection ends, or until there is nothing left
// to wait for.
//
// A connection that was never made ends here rather than at a program's
// exit status: there was no program. That is the same answer the pane
// gets from a shell that closed, which is what leaves the pane on
// screen with its account of why still in it.
func (c *connLog) Wait() error {
	select {
	case <-c.settled:
	case <-c.closed:
		return nil
	}
	c.mu.Lock()
	live := c.live
	c.mu.Unlock()
	if live != nil {
		return live.Wait()
	}
	return nil
}

// Close ends the log and the connection under it.
//
// Closing the pane before the connection is made is how the user gives
// up on it, so that is what this does.
func (c *connLog) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		c.mu.Lock()
		live := c.live
		c.over = true
		c.mu.Unlock()
		c.settle()
		if c.stop != nil {
			c.stop()
		}
		if live != nil {
			err = live.Close()
		}
	})
	return err
}
