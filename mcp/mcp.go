// Package mcp serves gridterm's panes to an agent over the Model
// Context Protocol.
//
// It translates and nothing more: an agent speaks JSON-RPC on this
// process's standard input and output, and the window speaks its own
// protocol on a loopback port. What an agent may ask for is decided by
// the window, and what it may reach by the code the user gave it.
//
// It runs as its own process, started by whatever runs the agent, and
// holds no credentials. A session code arrives in a tool call and opens
// one pane.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"sync"
)

// protocolVersion is the one version this speaks. An agent that asks
// for another is answered with this one, which is what the protocol
// says to do when the version asked for is not on offer.
const protocolVersion = "2025-06-18"

// longestLine caps one message, so a client that sends no newline
// cannot make this process hold an unbounded buffer.
const longestLine = 1 << 22

// request is one JSON-RPC message from the agent.
//
// A message with no ID is a notification: it is acted on and not
// answered.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response is one answer.
//
// ID is always written, even for a message so broken that there was no
// id to read. An empty one writes null, which is what says "this
// answers something, and I could not tell what".
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a failure in the protocol itself, as against a tool that
// ran and could not do what it was asked.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// The JSON-RPC codes this uses.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Panes is what this server can do with a window's panes.
//
// It is the whole of what an agent can reach. An implementation talks
// to one gridterm window; nothing here knows how.
type Panes interface {
	// Use takes a session code the user gave the agent and opens the
	// pane it names.
	Use(code string) (Pane, error)

	// List is the panes this agent has been given.
	List() ([]Pane, error)

	// Read is the last lines of a pane, ending at the bottom of the
	// screen. Zero lines is the screen, and more than it holds reaches
	// into what has scrolled off.
	Read(id string, lines int) (Screen, error)

	// Send types text into a pane and then presses the keys named in
	// keys. Either may be empty.
	Send(id, text string, keys []string) error

	// Wait watches a pane until it says what was asked for, goes quiet,
	// or the time runs out, and reports whether the time ran out. Lines
	// is how much of the pane to give back, as for Read.
	Wait(id string, lines int, until Until) (Screen, bool, error)

	// Close lets go of the window.
	Close() error
}

// Pane is one pane an agent has been given.
type Pane struct {
	ID    string `json:"pane"`
	Label string `json:"what"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
}

// Screen is a pane as it stands.
type Screen struct {
	Screen string `json:"screen"`
	Gone   bool   `json:"program_has_finished"`
}

// Until says what a wait is waiting for.
type Until struct {
	Contains  string
	QuietMS   int
	TimeoutMS int
}

// Serve answers an agent on one pair of streams until it goes.
//
// It returns when the agent closes its end, when ctx is cancelled, or
// when an answer could not be written: a client waiting for an id that
// will never be sent is a conversation that has ended whether or not it
// knows yet.
//
// What bounds what. Nothing bounds a read: an agent may think for as
// long as it likes between questions. So the reading happens on a
// goroutine of its own and the other two endings do not have to wait for
// it -- that goroutine is left parked on the read when Serve returns,
// because the stream belongs to whoever handed it in and only they can
// close it.
//
// Questions are answered as they come rather than one after another,
// because a wait can be minutes long and an agent has to be able to ask
// something else meanwhile. Only answersAtOnce run at a time; past that
// the next waits its turn, which stops reading and so slows the client
// down.
func Serve(ctx context.Context, in io.Reader, out io.Writer, panes Panes) (err error) {
	r := bufio.NewReaderSize(in, 4096)
	s := &server{panes: panes, out: json.NewEncoder(out), broke: make(chan struct{})}

	// Whatever is still being answered is waited for. The panes are not
	// closed here: they were handed in and whoever handed them in lets
	// go of them, which is what ends a question still in flight.
	var running sync.WaitGroup
	atOnce := make(chan struct{}, answersAtOnce)
	done := make(chan struct{})
	defer func() {
		close(done)
		running.Wait()
		if err == nil {
			// An answer written on one of those goroutines may have been
			// the one that failed.
			err = s.failed()
		}
	}()

	lines := make(chan []byte)
	stopped := make(chan error, 1)
	go func() {
		defer close(lines)
		for {
			line, err := readLine(r)
			switch {
			case errors.Is(err, io.EOF):
				return
			case errors.Is(err, errTooLong):
				// Read past and carried on. A message too long to be a
				// message is one bad message, not the end of the
				// conversation.
				s.send(fail(nil, codeParse, "that message is too long to read"))
				continue
			case err != nil:
				stopped <- fmt.Errorf("mcp: read: %w", err)
				return
			}
			if len(strings.TrimSpace(string(line))) == 0 {
				continue
			}
			select {
			case lines <- line:
			case <-done:
				return
			}
		}
	}()

	for {
		var line []byte
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.broke:
			return s.failed()
		case err := <-stopped:
			return err
		case got, ok := <-lines:
			if !ok {
				// End of file on the agent's side, which is how the
				// process running it says it is done.
				select {
				case err := <-stopped:
					return err
				default:
					return nil
				}
			}
			line = got
		}

		select {
		case atOnce <- struct{}{}:
			running.Add(1)
			go func() {
				defer running.Done()
				defer func() { <-atOnce }()
				s.answerOne(line)
			}()
		default:
			// As many are being answered as this will answer at once.
			// Done here instead, which stops reading until it is over.
			s.answerOne(line)
		}
	}
}

// answersAtOnce is how many questions are answered at the same time:
// enough that a wait does not stop an agent asking anything else, few
// enough that a flooding client cannot make this hold a goroutine per
// line.
const answersAtOnce = 8

// answerOne does one message and sends the answer, if it has one.
func (s *server) answerOne(line []byte) {
	answer, reply := s.handle(line)
	if !reply {
		return
	}
	s.send(answer)
}

// send writes one answer, under a lock: several can finish at once, and
// half of one answer inside another is a stream neither end can read.
func (s *server) send(answer response) {
	s.writing.Lock()
	defer s.writing.Unlock()
	if s.broken != nil {
		return
	}
	if err := s.out.Encode(answer); err != nil {
		// A write that failed on a stream still open leaves the client
		// waiting for an id nothing will ever answer, so the
		// conversation ends here rather than carrying on writing into
		// it.
		s.broken = fmt.Errorf("mcp: write: %w", err)
		if s.broke != nil {
			close(s.broke)
		}
	}
}

// failed returns the write that ended the conversation, if one did.
func (s *server) failed() error {
	s.writing.Lock()
	defer s.writing.Unlock()
	return s.broken
}

// errTooLong says a message was longer than a message can be. What is
// left of it has been read past, so the next read starts on the one
// after.
var errTooLong = errors.New("mcp: that message is too long to read")

// readLine reads one message, refusing one too long to be a message.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	tooLong := false
	for {
		part, more, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		if !tooLong {
			line = append(line, part...)
			if len(line) > longestLine {
				tooLong, line = true, nil
			}
		}
		if !more {
			if tooLong {
				return nil, errTooLong
			}
			return line, nil
		}
	}
}

// server answers one agent.
type server struct {
	panes Panes

	writing sync.Mutex
	out     *json.Encoder

	// broken is the write that ended the conversation, and broke is
	// closed when it happens so that a Serve waiting on a read wakes up.
	// Guarded by writing, because that is what one answer at a time is
	// written under.
	broken error
	broke  chan struct{}
}

// handle answers one message, and reports whether there is an answer to
// send: a notification is acted on and not answered.
func (s *server) handle(line []byte) (response, bool) {
	// Several messages in one array, which this protocol does not take.
	if trimmed := strings.TrimSpace(string(line)); strings.HasPrefix(trimmed, "[") {
		return fail(nil, codeInvalidRequest, "send one message at a time"), true
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return fail(nil, codeParse, "that is not a message this understands"), true
	}
	if req.JSONRPC != "2.0" {
		return fail(req.ID, codeInvalidRequest,
			`a message has to say "jsonrpc":"2.0"`), true
	}
	if req.Method == "" {
		return fail(req.ID, codeInvalidRequest, "a message has to say what it wants"), true
	}
	// A notification: acted on, never answered. Answering one is a
	// message the agent has nothing to match and will complain about.
	//
	// No id at all is a notification. An id that is null is not: it is a
	// question that named itself nothing, and an answer to it could
	// never be matched.
	if string(req.ID) == "null" {
		return fail(nil, codeInvalidRequest, "a question cannot call itself null"), true
	}
	notice := len(req.ID) == 0

	result, rerr := s.call(req)
	if notice {
		return response{}, false
	}
	if rerr != nil {
		return response{JSONRPC: "2.0", ID: req.ID, Error: rerr}, true
	}
	return response{JSONRPC: "2.0", ID: req.ID, Result: result}, true
}

// call does what a message asks.
func (s *server) call(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "gridterm",
				"title":   "gridterm panes",
				"version": version(),
			},
			"instructions": instructions,
		}, nil

	case "notifications/initialized", "notifications/cancelled":
		return map[string]any{}, nil

	case "ping":
		return map[string]any{}, nil

	case "tools/list":
		return map[string]any{"tools": toolList()}, nil

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: "that is not a tool call"}
		}
		got, rerr := s.runTool(params.Name, params.Arguments)
		if rerr != nil {
			return nil, rerr
		}
		return got, nil
	}
	return nil, &rpcError{
		Code:    codeMethodNotFound,
		Message: fmt.Sprintf("%q is not something this server does", req.Method),
	}
}

// fail is an answer that says what was wrong with the message.
func fail(id json.RawMessage, code int, why string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: why}}
}

// version is what this server calls itself: the version of the module
// it was built from, or "dev" for a build that has none.
func version() string {
	if built, ok := debug.ReadBuildInfo(); ok && built.Main.Version != "" {
		return built.Main.Version
	}
	return "dev"
}

// instructions is what an agent is told about this server when it
// connects. It carries the same workflow as the prompt the user pastes,
// without the code and without the lines that add this server to a host.
var instructions = strings.Join([]string{
	`gridterm hands you one terminal pane at a time.

The user sets a session up -- through whatever machines, as whatever
user -- and then gives you a session code for that one pane. Call
use_session_code with it before anything else. The answer names the
pane, and every other tool takes that name.`,
	Workflow,
	Rules,
}, "\n\n")

// Workflow is how an agent works in a pane it has been handed. This
// server's initialize answer and gridterm's hand-over prompt both carry
// it, so the two cannot drift apart.
const Workflow = `read_pane gives you the pane's screen as plain text, and takes lines to read that many,
back through what has scrolled off the top. send_keys types text in exactly as given, so a
command needs "\r" at the end for Enter, and presses the keys named in keys: Escape, Tab,
the arrows, F1 to F12, Ctrl+C. It does not wait, so call wait_for before you read again.
Give wait_for contains when you know what the screen will say, or quiet_ms to wait for the
screen to stop changing. It gives back the screen either way, and says when the time ran
out instead. list_panes lists the panes you have been handed, and that is all it lists.`

// Rules is what an agent may do in a pane it has been handed, and what
// it may not.
const Rules = `Work in that pane and nowhere else. It is a live shell running as whoever the user set it up
as, so it does whatever that shell does. Ask the user before anything destructive, the way
you would in somebody else's terminal. A password prompt is the user's to answer: ask them
to type it into the pane, wait with wait_for, and never type a password yourself. The user
watches this screen and can take the pane back at any moment, and then nothing here works.`
