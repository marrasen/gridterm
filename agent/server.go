package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
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
	// pane, so the window can show it. Called the same way as OnError.
	OnUse  func(id string)
	OnGone func(id string)
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

	// longestLine caps what one request may be, so an agent that sends
	// no newline cannot make this window hold an unbounded buffer.
	longestLine = 1 << 20

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

	// What this agent has been handed, which is only what it has given a
	// code for. One connection cannot reach another's panes, and a pane
	// the user has taken back fails on the next request.
	held := map[string]Pane{}
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
	first, err := readLine(in)
	if err != nil {
		return
	}
	var greeting ask
	if err := json.Unmarshal(first, &greeting); err != nil ||
		greeting.Do != "hello" || greeting.Protocol != hello {
		// Answered before hanging up, so an agent of another build is
		// told why rather than left guessing.
		_ = out.Encode(said{Error: "this window speaks " + hello})
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
		line, err := readLine(in)
		if err != nil {
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

// readLine reads one request, refusing one too long to be a request.
func readLine(in *bufio.Reader) ([]byte, error) {
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
		if len(line) > longestLine {
			return nil, fmt.Errorf("a request of %d bytes is not a request", len(line))
		}
		if !more {
			return line, nil
		}
	}
}

// answer does one request.
func (s *Server) answer(want ask, held map[string]Pane) said {
	switch want.Do {
	case "use":
		pane, err := s.cfg.Window.Use(want.Code)
		if err != nil {
			return said{Error: err.Error()}
		}
		held[pane.ID] = pane
		if s.cfg.OnUse != nil {
			s.cfg.OnUse(pane.ID)
		}
		return said{Pane: &pane}

	case "panes":
		out := make([]Pane, 0, len(held))
		for _, p := range held {
			out = append(out, p)
		}
		return said{Panes: out}

	case "read":
		if _, ok := held[want.Pane]; !ok {
			return said{Error: notHanded(want.Pane)}
		}
		look, err := s.cfg.Window.Look(want.Pane, want.Lines)
		if err != nil {
			return said{Error: err.Error()}
		}
		return said{Look: &look}

	case "send":
		if _, ok := held[want.Pane]; !ok {
			return said{Error: notHanded(want.Pane)}
		}
		if err := s.cfg.Window.Send(want.Pane, want.Text, want.Keys); err != nil {
			return said{Error: err.Error()}
		}
		return said{OK: true}

	case "wait":
		if _, ok := held[want.Pane]; !ok {
			return said{Error: notHanded(want.Pane)}
		}
		return s.waitFor(want)
	}
	return said{Error: fmt.Sprintf("%q is not something this window does", want.Do)}
}

// notHanded is what an agent is told about a pane it was never given.
func notHanded(id string) string {
	return fmt.Sprintf("%q is not a pane you have been handed", id)
}

// waitFor watches a pane until it says what was asked for, goes quiet,
// or the time runs out.
//
// It watches the screen and nothing more, however many lines the agent
// asked for, because a wait asks twenty times a second and reading
// lines means rendering them. Those lines are read once, at the end.
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

		if want.Until.Contains != "" {
			if strings.Contains(look.Screen, want.Until.Contains) {
				return s.ending(want, look, false)
			}
		} else if now.Sub(lastMove) >= quiet {
			return s.ending(want, look, false)
		}
		// A program that has finished says nothing more, so there is
		// nothing left to wait for whichever way the wait was asked.
		if look.Gone {
			return s.ending(want, look, false)
		}
		// The window is shutting down, so what is on the screen now is
		// the last thing there will ever be to say about it.
		if now.After(deadline) || s.isClosed() {
			return s.ending(want, look, true)
		}
		time.Sleep(lookEvery)
	}
}

// ending is what a wait answers with: the lines the agent asked for,
// read now that the waiting is over. A wait that asked for no lines
// answers with the screen it was already watching.
func (s *Server) ending(want ask, look Look, waited bool) said {
	if want.Lines > 0 {
		full, err := s.cfg.Window.Look(want.Pane, want.Lines)
		if err != nil {
			return said{Error: err.Error()}
		}
		look = full
	}
	return said{Look: &look, Waited: waited}
}
