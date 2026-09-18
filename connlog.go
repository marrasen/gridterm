package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/session"
)

// connLog is a session that shows a connection being made and then
// becomes that connection.
//
// A pane opens on it straight away, so there is somewhere to watch from
// while the connecting happens: each step as it is tried, and whatever
// the far end says, including a sign-in link. When the connection is
// made the account is folded away -- the pane is cleared and one line
// takes its place -- and the same pane carries the shell.
//
// The whole account is kept in lines, which Lines hands back and the
// "How it was reached" dialog shows.
//
// The pane cannot tell. It reads a session, and this is one: first its
// own words, then the shell's.
type connLog struct {
	// clock says what time it is, so a test can have one that does not
	// move.
	clock func() time.Time

	// started is when the log was opened, for saying how long the
	// connection took.
	started time.Time

	// stop is called when the pane is closed before the connection was
	// made, which is how closing the pane gives up on it.
	stop func()

	// keep says the pane already holds a transcript the user asked to
	// keep, so the account is folded away without clearing it.
	keep bool

	mu sync.Mutex
	// said is what has been written and not yet read.
	said []byte
	// lines is every line written, in order, without the colour it was
	// shown in. The pane keeps no copy, so this is the account.
	lines []string
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
		started: time.Now(),
		stop:    stop,
		wake:    make(chan struct{}, 1),
		settled: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

// What a line is drawn in: the time in dark green on a line that went
// well and dark red on one that did not, and this window's own words in
// a grey darker than a shell's output.
const (
	sgrWent  = "\x1b[32m"
	sgrWrong = "\x1b[31m"
	sgrWords = "\x1b[90m"
	sgrOff   = "\x1b[0m"
)

// clearPane empties a terminal's screen and puts the cursor back at the
// top.
//
// The lines it takes off the screen stay in the pane's history, the way
// any cleared screen does now, so the account is a scroll away as well
// as being on the "How it was reached" dialog. What the fold is for is a
// pane that reads as one line, and that is what this leaves.
const clearPane = "\x1b[2J\x1b[3J\x1b[H"

// Say writes a line into the pane, stamped with the time.
//
// Called from whichever goroutine is doing the connecting, which is not
// the one that draws: what it writes is read by the pane's own reader
// like anything else a program says.
func (c *connLog) Say(what string) { c.say(what, false) }

// sayBadly is Say for a line that reports something going wrong, which
// stamps the time in red rather than green.
func (c *connLog) sayBadly(what string) { c.say(what, true) }

// say writes a stamped line, colouring the time by whether it went
// wrong.
//
// The line is cleaned first, because some of what is said here is the
// far end's own wording passed on by the dial, and the colour below
// would otherwise be a hostile server's to forge.
func (c *connLog) say(what string, wrong bool) {
	what = oneLine(what)
	now := c.clock().Format("15:04:05")
	stamp := sgrWent
	if wrong {
		stamp = sgrWrong
	}
	c.emit(now+"  "+what, stamp+now+sgrOff+"  "+sgrWords+what+sgrOff)
}

// oneLine is a line as it may be shown: nothing a terminal acts on, and
// nothing that breaks one line into two.
//
// Every way into the log goes through it, because the far end's words
// reach this window by more routes than the ones it quotes: the dial
// hands on what a server says it will accept, and a reason a connection
// failed often carries the server's own wording inside it.
func oneLine(s string) string {
	return strings.ReplaceAll(serve.Plain(s), "\n", " ")
}

// Lines is the whole account, in the order it was said, without the
// colour it was shown in.
func (c *connLog) Lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.lines)
}

// Quote writes what the far end said, marked as its words rather than
// this window's.
//
// Said in full. A server that signs people in through a browser sends a
// link this way, and a link that is cut off or that cannot be copied is
// a link nobody can use.
//
// Split on the line breaks it came with, because write keeps a line a
// line: what came on several is meant to be read on several.
func (c *connLog) Quote(who, what string) {
	what = strings.TrimRight(what, "\r\n")
	if strings.TrimSpace(what) == "" {
		return
	}
	c.Say(who + " says:")
	for line := range strings.SplitSeq(what, "\n") {
		c.write("    " + line)
	}
}

// Failed says why the connection was not made. Nothing more is written
// after it, and the pane stays so the reason can be read.
func (c *connLog) Failed(err error) {
	if err == nil {
		return
	}
	// Split first, because Say writes one line and a reason that came on
	// several is meant to be read on several.
	for line := range strings.SplitSeq(err.Error(), "\n") {
		c.sayBadly(line)
	}
	c.write("")
	c.note("The connection was not made. This pane is only the record of")
	c.note("it: close it when you have read it.")
	c.end()
}

// Refused says the machine answered but what was asked for on it could
// not be opened.
//
// Not Failed: the connection was made and the window is holding it, so
// an account saying it was not made would be about a live machine.
func (c *connLog) Refused(name, what string, err error) {
	if err == nil {
		return
	}
	// Split first, for the reason Failed gives.
	for line := range strings.SplitSeq(err.Error(), "\n") {
		c.sayBadly(line)
	}
	c.write("")
	c.note(name + " is connected, but " + what + " could not be opened.")
	c.note("This pane is the record of that: close it when you have read it.")
	c.end()
}

// Connected says the connection was made with nothing to ride in the
// pane, which is what a connection opened for files alone is.
//
// Nothing more will be written, and closing the pane lets go of the
// account rather than giving up on the connection under it.
func (c *connLog) Connected() {
	c.mu.Lock()
	c.stop = nil
	c.mu.Unlock()
	c.end()
}

// GaveUp says the user gave up on the connection.
func (c *connLog) GaveUp() {
	c.sayBadly("given up on")
	c.end()
}

// Became hands the pane over to the connection that was made, under the
// name it was reached as.
//
// The account is folded away first: the pane is cleared, screen and
// scrollback, and one line says how long it took and where to read the
// rest. From here the pane reads and writes the real thing.
func (c *connLog) Became(name string, live session.Session) {
	c.mu.Lock()
	if c.over || c.live != nil {
		c.mu.Unlock()
		// Given up on, or already handed over. Whatever arrived is
		// nobody's now, and closing it is all that is left to do with
		// it.
		_ = live.Close()
		return
	}
	// Read drains said before it looks at live, so the fold has to be in
	// said before live is set: after it, the shell's first screen could
	// be read first and then cleared.
	c.foldLocked(name)
	c.live = live
	cols, rows := c.cols, c.rows
	c.mu.Unlock()
	// Woken here rather than after the resize below, which goes to the
	// far end and takes as long as that takes.
	c.signal()
	c.settle()

	// The size the pane already had. A shell that started at eighty by
	// twenty-four and was told the truth afterwards has drawn its first
	// screen wrong.
	if cols > 0 && rows > 0 {
		if err := live.Resize(cols, rows); err != nil {
			c.sayBadly("could not tell it how big this pane is: " + err.Error())
		}
	}
	c.signal()
}

// foldLocked replaces what the pane is showing with one line, with the
// log's mutex already held.
//
// The summary is the pane's, not the account's, so it is not one of the
// lines: it says where to read them.
func (c *connLog) foldLocked(name string) {
	line := "Connected to " + name + " " + howLong(c.clock().Sub(c.started)) +
		`. "How it was reached", on the plus menu of its row, shows the account.`
	if !c.keep {
		c.said = append(c.said, clearPane...)
	}
	c.said = append(c.said, []byte(sgrWords+line+sgrOff+"\r\n")...)
}

// howLong says how long something took, in the words a summary line
// reads with.
func howLong(d time.Duration) string {
	if d < time.Second {
		return "in under a second"
	}
	return fmt.Sprintf("in %.1f s", d.Seconds())
}

// note writes one of this window's own lines that carries no time, in
// the same grey its stamped words are written in.
func (c *connLog) note(line string) {
	line = oneLine(line)
	c.emit(line, sgrWords+line+sgrOff)
}

// write adds one line with no colour on it, which is how the far end's
// own words are told apart from this window's.
func (c *connLog) write(line string) {
	line = oneLine(line)
	c.emit(line, line)
}

// emit adds one line, keeping the plain text and showing the coloured
// one.
func (c *connLog) emit(plain, shown string) {
	c.mu.Lock()
	if !c.over {
		c.lines = append(c.lines, plain)
		c.said = append(c.said, []byte(shown+"\r\n")...)
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

// errNeverConnected is what a connection that was never made ends with.
//
// Not nil, because nil is a program that ended cleanly, and a pane that
// said its command exited zero when the machine never answered would be
// telling the user the run went well.
var errNeverConnected = errors.New("the connection was not made")

// Wait blocks until the connection ends, or until there is nothing left
// to wait for.
//
// A connection that was never made ends here rather than at a program's
// exit status: there was no program. The pane is left on screen with its
// account of why still in it, the same as a shell that closed.
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
	return errNeverConnected
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
		live, stop := c.live, c.stop
		c.over = true
		c.mu.Unlock()
		c.settle()
		if stop != nil {
			stop()
		}
		if live != nil {
			err = live.Close()
		}
	})
	return err
}
