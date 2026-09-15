// Package mcp serves gridterm's panes to an agent over the Model
// Context Protocol.
//
// It is a translator and nothing else. An agent speaks JSON-RPC on this
// process's standard input and output; the window speaks its own
// protocol on a loopback port. What may be asked for is decided by the
// window, and what an agent may reach is decided by the code the user
// gave it.
//
// It runs as its own process, started by whatever is running the agent,
// and holds no credentials of its own. A session code arrives in a tool
// call, is used to open one pane, and is never written anywhere.
package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// The protocol this speaks. An agent that asks for another version is
// told this one: the shape of what follows is the same, and saying so
// is better than refusing to talk.
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
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
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

	// Read is what is on a pane now.
	Read(id string) (Screen, error)

	// Send types into a pane.
	Send(id, text string) error

	// Wait watches a pane until it says what was asked for, goes quiet,
	// or the time runs out, and reports whether the time ran out.
	Wait(id string, until Until) (Screen, bool, error)

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
// It returns when the agent closes its end, which is how the process
// running it says it is done.
func Serve(in io.Reader, out io.Writer, panes Panes) error {
	r := bufio.NewReaderSize(in, 4096)
	w := json.NewEncoder(out)
	s := &server{panes: panes}
	for {
		line, err := readLine(r)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("mcp: read: %w", err)
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		answer, reply := s.handle(line)
		if !reply {
			continue
		}
		if err := w.Encode(answer); err != nil {
			return fmt.Errorf("mcp: answer: %w", err)
		}
	}
}

// readLine reads one message, refusing one too long to be a message.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, more, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		line = append(line, part...)
		if len(line) > longestLine {
			return nil, fmt.Errorf("a message of %d bytes is not a message", len(line))
		}
		if !more {
			return line, nil
		}
	}
}

// server answers one agent.
type server struct{ panes Panes }

// handle answers one message, and reports whether there is an answer to
// send: a notification is acted on and not answered.
func (s *server) handle(line []byte) (response, bool) {
	if trimmed := strings.TrimSpace(string(line)); strings.HasPrefix(trimmed, "[") {
		// JSON-RPC allows several messages in one array; this protocol
		// dropped that, and answering one would mean guessing which id
		// the answer belonged to.
		return fail(nil, codeInvalidRequest, "send one message at a time"), true
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return fail(nil, codeParse, "that is not a message this understands"), true
	}
	if req.Method == "" {
		return fail(req.ID, codeInvalidRequest, "a message has to say what it wants"), true
	}
	// A notification: acted on, never answered. Answering one is a
	// message the agent has nothing to match and will complain about.
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
				"version": Version,
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
		return s.runTool(params.Name, params.Arguments), nil
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

// Version is what this server calls itself. Set by whatever builds it.
var Version = "dev"

// instructions is what an agent is told about this server when it
// connects.
const instructions = `gridterm hands you one terminal pane at a time.

The user sets a session up -- through whatever machines, as whatever
user -- and then gives you a session code for that one pane. Call
use_session_code with it before anything else.

You can read the pane, type into it, and wait for it to settle. You
cannot open connections, start shells, read files, or reach any other
pane. The user is watching the same screen and can take it back at any
moment, and then everything here stops working.

Type as a person would: send_keys puts characters in exactly as given,
so a command needs a carriage return ("\r") at the end. After sending a
command, call wait_for rather than read_pane: a read taken straight
afterwards shows the screen before the command has done anything.`
