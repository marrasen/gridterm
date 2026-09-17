package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config is what listening for agents takes.
type Config struct {
	// Window is what an agent may do with the panes handed to it. It is
	// never nil: a listener with nothing behind it would accept a code
	// and then have nothing to answer with.
	Window Window

	// OnError is told about a failure nothing else can be told about,
	// such as an agent's connection breaking. It is called from the
	// goroutine that found it, so an implementation that touches the
	// window has to hand the work to whatever draws.
	OnError func(error)

	// OnUse and OnGone say when an agent starts and stops working in a
	// share, so the window can show it on the rows of its panes. Called
	// the same way as OnError.
	OnUse  func(share uint64)
	OnGone func(share uint64)
}

// Server listens for agents on the loopback address.
type Server struct {
	cfg Config
	ln  net.Listener

	mu      sync.Mutex
	closed  bool
	talking map[net.Conn]struct{}

	// accepting is the goroutine taking connections, and nothing else.
	// The goroutines answering agents are not waited for; see Close.
	accepting sync.WaitGroup
}

// The defaults a wait uses when the agent asks for none.
const (
	// quietFor is how long a pane has to say nothing before a wait
	// decides whatever was running has finished.
	quietFor = 700 * time.Millisecond

	// giveUpAfter is how long a wait goes on before saying what is on
	// the screen anyway.
	giveUpAfter = 30 * time.Second

	// longestWait caps what an agent may ask for, so one cannot park a
	// goroutine of this window for an afternoon.
	longestWait = 5 * time.Minute

	// acceptAgain is how long to wait before taking connections again
	// after a failure to take one. Long enough not to spin on a machine
	// that has run out of something, short enough that an agent
	// arriving in the meantime only waits a moment.
	acceptAgain = 50 * time.Millisecond

	// lookEvery is how often a wait asks what is on the screen. Often
	// enough to feel immediate, rarely enough that a wait costs the
	// window nothing.
	lookEvery = 50 * time.Millisecond

	// stuckIsQuietFor is how many times the ordinary quiet a pane has to
	// say nothing for before a wait gives up on a shell that still says
	// a command is running.
	stuckIsQuietFor = 10

	// settledFor is how long a pane has to say nothing before a prompt
	// that looks like the one the agent typed at is taken for the real
	// thing. Long enough to be past the end of a chunk of output, short
	// enough that a finish is still answered at once.
	settledFor = 150 * time.Millisecond

	// sayHelloWithin is how long a connection has to say what it is
	// before it is hung up on. A goroutine and a buffer held by
	// something that says nothing is a goroutine and a buffer nobody
	// asked for.
	sayHelloWithin = 10 * time.Second

	// mostAgents is how many may be connected at once.
	//
	// A handover is one user giving one pane to one agent, so this is
	// far more than anybody uses. It is a cap because a wait costs a
	// goroutine and a question of the window twenty times a second, and
	// anything running as this user can open a connection.
	mostAgents = 8
)

// How long a line on the wire may be, each way, so that neither end can
// be made to hold an unbounded buffer.
//
// The two differ because what they carry does. A request carries what an
// agent types, which is a command line and not a file. An answer carries
// a screen, and has to hold MostLines rows of the widest pane anybody
// has after JSON escaping: five hundred rows of eight hundred columns at
// six bytes an escaped character is two and a half million, and the
// limit leaves room over that.
const (
	// LongestRequest caps one request, and is what anything sending
	// requests has to keep its own messages under.
	LongestRequest = 1 << 21

	// longestAnswer caps one answer.
	longestAnswer = 1 << 23
)

// Listen starts listening for agents on a port of the system's
// choosing, on the loopback address only. What gets in past that is
// decided by a code the user handed over.
func Listen(cfg Config) (*Server, error) {
	if cfg.Window == nil {
		return nil, errors.New("agent: there is nothing to serve")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("agent: listen: %w", err)
	}
	s := &Server{cfg: cfg, ln: ln, talking: map[net.Conn]struct{}{}}
	s.accepting.Add(1)
	go func() {
		defer s.accepting.Done()
		s.accept()
	}()
	return s, nil
}

// Port is the port agents reach this window on.
func (s *Server) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// Close stops listening and hangs up on every agent.
//
// It does not wait for the goroutines answering agents. Every question
// an agent asks is answered by the window, and taking a pane back
// happens in the window too: waiting here would be the window waiting
// for something that is waiting for the window. Those goroutines end as
// soon as what they are waiting on gives up, and nothing depends on
// their having finished.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	talking := make([]net.Conn, 0, len(s.talking))
	for c := range s.talking {
		talking = append(talking, c)
	}
	s.mu.Unlock()

	errs := []error{s.ln.Close()}
	for _, c := range talking {
		errs = append(errs, c.Close())
	}
	s.accepting.Wait()
	return errors.Join(errs...)
}

// accept takes agents until the listener is closed.
func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			if s.isClosed() {
				return
			}
			// Said and tried again. Running out of handles is a
			// moment, and a listener that gave up on one would leave
			// the row saying a pane is offered with nothing behind it.
			s.onError(fmt.Errorf("agent: accept: %w", err))
			time.Sleep(acceptAgain)
			continue
		}
		if !s.hold(c) {
			_ = c.Close()
			continue
		}
		go s.talk(c)
	}
}

// hold records a connection, reporting whether it may stay: a server
// that has closed takes nobody, and nor does one already talking to as
// many agents as it will.
func (s *Server) hold(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.talking) >= mostAgents {
		return false
	}
	s.talking[c] = struct{}{}
	return true
}

// drop forgets a connection.
func (s *Server) drop(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.talking, c)
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Server) onError(err error) {
	if s.cfg.OnError != nil {
		s.cfg.OnError(err)
	}
}

// talk answers one agent until it goes.
func (s *Server) talk(c net.Conn) {
	defer func() {
		s.drop(c)
		if err := c.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			s.onError(fmt.Errorf("agent: close: %w", err))
		}
	}()

	// The shares this agent has given a code for. A pane's name begins
	// with its share, so a connection cannot reach another's panes, and
	// a share the user has ended fails on the next request.
	held := map[uint64]bool{}
	defer func() {
		for id := range held {
			if s.cfg.OnGone != nil {
				s.cfg.OnGone(id)
			}
		}
	}()

	in := bufio.NewReaderSize(c, 4096)
	out := json.NewEncoder(c)

	// Nothing is answered until the first line says what this is. A
	// connection that says something else, or says nothing, is hung up
	// on rather than talked to: see hello.
	if err := c.SetReadDeadline(time.Now().Add(sayHelloWithin)); err != nil {
		s.onError(fmt.Errorf("agent: set a deadline: %w", err))
		return
	}
	first, err := readLine(in, LongestRequest)
	if err != nil {
		return
	}
	var greeting ask
	if err := json.Unmarshal(first, &greeting); err != nil ||
		greeting.Do != "hello" || greeting.Protocol != hello {
		// Answered before hanging up, so an agent of another build is
		// told why rather than left guessing.
		_ = out.Encode(said{Error: whichIsOlder(greeting.Protocol)})
		return
	}
	if err := out.Encode(said{OK: true}); err != nil {
		return
	}
	if err := c.SetReadDeadline(time.Time{}); err != nil {
		s.onError(fmt.Errorf("agent: clear a deadline: %w", err))
		return
	}

	for {
		line, err := readLine(in, LongestRequest)
		if err != nil {
			// A request too long to be a request is said before hanging
			// up, so an agent that sent one is told why.
			if errors.Is(err, errTooLong) {
				_ = out.Encode(said{Error: err.Error()})
			}
			if !errors.Is(err, errGone) && !s.isClosed() {
				s.onError(fmt.Errorf("agent: read: %w", err))
			}
			return
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var want ask
		if err := json.Unmarshal(line, &want); err != nil {
			// Hung up on rather than answered. A stream that has stopped
			// making sense is not an agent, and one that is answered and
			// read from again is one a browser can be made to send.
			_ = out.Encode(said{Error: "that is not something this window understands"})
			return
		}
		if err := out.Encode(s.answer(want, held)); err != nil {
			// The agent has gone mid-answer. Nothing here can do
			// anything about it and nothing is waiting on it.
			return
		}
	}
}

// errGone says the agent hung up, which is not a failure.
var errGone = errors.New("agent: the agent has gone")

// errTooLong says a line was longer than the limit for that direction.
var errTooLong = errors.New("agent: that is too long to read")

// readLine reads one line, refusing one longer than most.
func readLine(in *bufio.Reader, most int) ([]byte, error) {
	var line []byte
	for {
		part, more, err := in.ReadLine()
		if err != nil {
			// An agent that hung up, a connection closed from here, or
			// one that ran out of time to say what it was: all of them
			// are this conversation ending rather than going wrong.
			// Anything else is a failure and is said.
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) ||
				errors.Is(err, os.ErrDeadlineExceeded) {
				return nil, errGone
			}
			return nil, err
		}
		line = append(line, part...)
		if len(line) > most {
			return nil, fmt.Errorf("%w: %d bytes, and the most is %d", errTooLong, len(line), most)
		}
		if !more {
			return line, nil
		}
	}
}

// answer does one request.
func (s *Server) answer(want ask, held map[uint64]bool) said {
	switch want.Do {
	case "use":
		sh, err := s.cfg.Window.Use(want.Code)
		if err != nil {
			return said{Error: err.Error()}
		}
		if !held[sh.ID] {
			held[sh.ID] = true
			if s.cfg.OnUse != nil {
				s.cfg.OnUse(sh.ID)
			}
		}
		return said{Share: &sh}

	case "panes":
		// Asked of the window rather than remembered, because the user
		// adds panes and takes them out while the agent works: what it
		// wants is what it has now.
		out := make([]Pane, 0, len(held))
		for id := range held {
			panes, err := s.cfg.Window.Shared(id)
			if errors.Is(err, ErrShareOver) {
				// The agent asked what it has, and it has the rest.
				continue
			}
			if err != nil {
				// Anything else is the window failing to answer, which
				// is not the same as having nothing.
				return said{Error: err.Error()}
			}
			out = append(out, panes...)
		}
		return said{Panes: out}

	case "read":
		if !holds(held, want.Pane) {
			return said{Error: notHanded(want.Pane)}
		}
		look, err := s.cfg.Window.Look(want.Pane, want.Lines)
		if err != nil {
			return said{Error: err.Error()}
		}
		return said{Look: &look}

	case "output":
		if !holds(held, want.Pane) {
			return said{Error: notHanded(want.Pane)}
		}
		look, err := s.cfg.Window.Output(want.Pane, want.Lines)
		if err != nil {
			return said{Error: err.Error()}
		}
		return said{Look: &look}

	case "send":
		if !holds(held, want.Pane) {
			return said{Error: notHanded(want.Pane)}
		}
		// Refused before anything is typed, so an agent that spelled a
		// key wrong or sent too many has sent nothing.
		if err := CheckKeys(want.Keys); err != nil {
			return said{Error: err.Error()}
		}
		if err := s.cfg.Window.Send(want.Pane, want.Text, want.Keys); err != nil {
			return said{Error: err.Error()}
		}
		return said{OK: true}

	case "wait":
		if !holds(held, want.Pane) {
			return said{Error: notHanded(want.Pane)}
		}
		return s.waitFor(want)

	case "restart":
		if !holds(held, want.Pane) {
			return said{Error: notHanded(want.Pane)}
		}
		pane, err := s.cfg.Window.Restart(want.Pane)
		if err != nil {
			return said{Error: err.Error()}
		}
		// The same pane, so the same name: what changed is what is
		// running in it.
		return said{Pane: &pane}

	case "secret":
		if !holds(held, want.Pane) {
			return said{Error: notHanded(want.Pane)}
		}
		wait := time.Duration(want.WaitMS) * time.Millisecond
		if wait <= 0 || wait > LongestSecretWait {
			wait = LongestSecretWait
		}
		typed, err := s.cfg.Window.Secret(want.Pane, want.Text, wait)
		if err != nil {
			return said{Error: err.Error()}
		}
		return said{OK: true, Typed: typed}

	case "open":
		if !holds(held, want.Pane) {
			return said{Error: notHanded(want.Pane)}
		}
		pane, err := s.cfg.Window.Open(want.Pane)
		if err != nil {
			return said{Error: err.Error()}
		}
		// In the same share as the pane it was opened from, so this
		// agent holds it already.
		return said{Pane: &pane}
	}
	return said{Error: fmt.Sprintf("%q is not something this window does", want.Do)}
}

// holds reports whether a pane belongs to a share this agent has given a
// code for.
//
// The share is the whole of the check here. Whether the pane is still in
// it, and still open, is the window's to say on the request itself.
func holds(held map[uint64]bool, pane string) bool {
	share, ok := ShareOf(pane)
	return ok && held[share]
}

// notHanded is what an agent is told about a pane it was never given.
func notHanded(id string) string {
	return fmt.Sprintf("%q is not a pane you have been handed", id)
}

// whichIsOlder is what a connection speaking another protocol is told,
// naming the build to update rather than only the version it wanted.
func whichIsOlder(spoke string) string {
	said := "this window speaks " + hello + "."
	theirs, ok := protocolNumber(spoke)
	if !ok {
		return said + " That greeting is not one of gridterm's."
	}
	switch mine, _ := protocolNumber(hello); {
	case theirs < mine:
		return said + " The gridterm serving this MCP server is an older build than this window."
	case theirs > mine:
		return said + " This window is an older build than the gridterm serving this MCP server."
	}
	return said
}

// protocolNumber is the version out of a greeting, and whether it was
// one of gridterm's at all.
func protocolNumber(spoke string) (int, bool) {
	rest, ok := strings.CutPrefix(spoke, "gridterm-agent-")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

// waitFor watches a pane until it says what was asked for, goes quiet,
// or the time runs out.
//
// It watches the screen and nothing more, however many lines the agent
// asked for, because a wait asks twenty times a second and reading
// lines means rendering them. Those lines are read twice: once before
// the waiting starts, to see what the pane was already holding, and once
// at the end.
func (s *Server) waitFor(want ask) said {
	quiet := quietFor
	if want.Until.QuietMS > 0 {
		quiet = time.Duration(want.Until.QuietMS) * time.Millisecond
	}
	upTo := giveUpAfter
	if want.Until.TimeoutMS > 0 {
		upTo = time.Duration(want.Until.TimeoutMS) * time.Millisecond
	}
	if upTo > longestWait {
		upTo = longestWait
	}

	// What the pane already held, for a wait that will search the lines
	// it reads at the end. Text that was there before the waiting began
	// is not what the wait was waiting for.
	var was witness
	if want.Until.Contains != "" && want.Lines > 0 {
		// A reading that failed is no reading at all, and the waiting goes
		// on without one: an empty screen makes every line at the end look
		// new, which is worse than narrowing nothing.
		if look, err := s.cfg.Window.Look(want.Pane, want.Lines); err == nil {
			was = witness{screen: look.Screen, cols: look.Cols, rows: look.Rows, read: true}
		}
	}

	deadline := time.Now().Add(upTo)
	var (
		last     Look
		lastMove = time.Now()
		first    = true
	)
	for {
		look, err := s.cfg.Window.Look(want.Pane, 0)
		if err != nil {
			return said{Error: err.Error()}
		}
		now := time.Now()
		if first || look.Changed != last.Changed {
			lastMove, first = now, false
		}
		last = look

		switch {
		// The shell's own marks come first, and end the wait even when
		// the agent asked for text: a command that has finished will not
		// print that text now, and the agent is better told so than left
		// until the time runs out.
		//
		// Since the agent last typed, not since this wait began. Sending
		// keys and waiting are two calls, and anything short -- ls, echo,
		// git status -- has finished before the second one arrives.
		case look.Marks && !look.Running && look.Yours:
			return s.ending(want, look, was, false, EndedOnMarks)
		case want.Until.Contains != "":
			if strings.Contains(look.Screen, want.Until.Contains) {
				return s.ending(want, look, was, false, EndedOnText)
			}
		// A shell that marks nothing, where the prompt coming back is
		// what a finish looks like. It has to have been quiet for a
		// moment first: mid-output the cursor sits wherever the last
		// chunk of bytes left it, and a line that happens to read like
		// the prompt is not the prompt.
		case look.Watching && look.Back:
			if now.Sub(lastMove) >= settledFor {
				return s.ending(want, look, was, false, EndedOnPrompt)
			}
		// A pane that has stopped saying anything. A shell that says a
		// command is still running is given far longer, because a
		// command that thinks before it prints goes quiet at once and is
		// not finished; the longer wait is there because a mark can be
		// left stuck running for ever, and waiting out the whole timeout
		// for one is worse than saying what happened.
		case look.Marks && look.Running:
			if now.Sub(lastMove) >= quiet*stuckIsQuietFor {
				return s.ending(want, look, was, false, EndedOnStuck)
			}
		case now.Sub(lastMove) >= quiet:
			return s.ending(want, look, was, false, EndedOnQuiet)
		}
		// A program that has finished says nothing more, so there is
		// nothing left to wait for whichever way the wait was asked.
		if look.Gone {
			return s.ending(want, look, was, false, EndedOnGone)
		}
		// The window is shutting down, so what is on the screen now is
		// the last thing there will ever be to say about it.
		if now.After(deadline) || s.isClosed() {
			return s.ending(want, look, was, true, EndedOnTime)
		}
		time.Sleep(lookEvery)
	}
}

// witness is the reading a wait takes before it starts, so that what it
// finds at the end can be narrowed to the lines that arrived while it
// was waiting.
type witness struct {
	// screen is what the lines the agent asked for held then.
	screen string

	// cols and rows are the size of the pane that screen was read at. A
	// reading taken at another size does not line up with this one.
	cols, rows int

	// read says the reading was taken at all; one that failed narrows
	// nothing.
	read bool
}

// ending is what a wait answers with: the lines the agent asked for,
// read now that the waiting is over. A wait that asked for no lines
// answers with the screen it was already watching.
//
// was is the reading taken before the waiting began.
func (s *Server) ending(want ask, look Look, was witness, waited bool, because string) said {
	if want.Lines <= 0 {
		return said{Look: &look, Waited: waited, Because: because}
	}
	full, err := s.cfg.Window.Look(want.Pane, want.Lines)
	if err != nil {
		// The waiting is over and the screen in hand is what there was
		// at the end of it. A pane the user took back between the two
		// reads must not turn a finished wait into nothing at all.
		look.Note = "the " + strconv.Itoa(want.Lines) +
			" lines you asked for could not be read: " + err.Error()
		return said{Look: &look, Waited: waited, Because: because}
	}
	// What was waited for may have gone past the top of the screen
	// between two looks, and the watching only ever sees the screen. Only
	// in the lines that arrived while the wait was on: text the pane was
	// already holding is not something the wait saw happen.
	//
	// A pane resized while the wait was on was read at two sizes, so its
	// lines were wrapped differently and no longer line up. Nothing is
	// narrowed then, and the wait stands as it ended.
	sameSize := was.cols == full.Cols && was.rows == full.Rows
	if waited && want.Until.Contains != "" && was.read && sameSize &&
		strings.Contains(addedSince(was.screen, full.Screen), want.Until.Contains) {
		waited, because = false, EndedOnText
	}
	return said{Look: &full, Waited: waited, Because: because}
}

// addedSince is at most the part of a reading of a pane that was not in
// an earlier reading of the same pane.
//
// Both are the last lines of one pane, and a pane only ever grows at the
// bottom, so the later reading begins with lines the earlier one already
// had. The longest run of the earlier one's lines that the later one
// starts with is taken as old, and what follows is new. A run that stops
// short of the earlier reading's end is its bottom row having been
// written over, which is new too. Nothing in common means a whole
// reading's worth has scrolled past and all of it is new.
//
// That longest run is a bound rather than an answer. A pane repeating a
// block of lines exactly, such as the same command run twice, matches a
// longer run than really carried over, so what comes back is at most the
// new text and can be less.
func addedSince(was, now string) string {
	// An earlier reading of nothing carried nothing over. Splitting an
	// empty string gives one empty line, which would swallow the first new
	// line whenever that line is empty too.
	if was == "" {
		return now
	}
	had := strings.Split(was, "\n")
	has := strings.Split(now, "\n")
	old := 0
	for i := range had {
		n := 0
		for i+n < len(had) && n < len(has) && had[i+n] == has[n] {
			n++
		}
		old = max(old, n)
	}
	return strings.Join(has[old:], "\n")
}
