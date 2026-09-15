package mcp

import (
	"encoding/json"
	"fmt"
)

// tool is one thing an agent can ask for.
type tool struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	InputSchema schema `json:"inputSchema"`
}

// schema says what a tool's arguments look like.
type schema struct {
	Type       string           `json:"type"`
	Properties map[string]field `json:"properties"`
	Required   []string         `json:"required,omitempty"`
}

// field is one argument.
type field struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// result is what a tool call gives back: text, because what an agent is
// given is a screen.
type result struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toolList is what this server offers.
func toolList() []tool {
	return []tool{
		{
			Name:  "use_session_code",
			Title: "Use a session code",
			Description: "Open the pane a session code names. The user makes the code in" +
				" gridterm and gives it to you; it is the only way to reach anything here." +
				" Call this before any other tool.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"code": {Type: "string", Description: "the session code the user gave you"},
				},
				Required: []string{"code"},
			},
		},
		{
			Name:        "list_panes",
			Title:       "List the panes you have",
			Description: "The panes the user has handed you, and how big each screen is.",
			InputSchema: schema{Type: "object", Properties: map[string]field{}},
		},
		{
			Name:  "read_pane",
			Title: "Read a pane",
			Description: "What is on the pane's screen now, as plain text. After sending a" +
				" command, use wait_for instead: a read taken straight afterwards shows the" +
				" screen before the command has done anything.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string", Description: "which pane, from use_session_code"},
				},
				Required: []string{"pane"},
			},
		},
		{
			Name:  "send_keys",
			Title: "Type into a pane",
			Description: "Put characters into the pane exactly as given, as though typed" +
				" there. A command needs a carriage return (\\r) at the end to be run." +
				" Control characters work: \\u0003 is ctrl+c.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string", Description: "which pane, from use_session_code"},
					"text": {Type: "string", Description: "what to type"},
				},
				Required: []string{"pane", "text"},
			},
		},
		{
			Name:  "wait_for",
			Title: "Wait for a pane",
			Description: "Watch a pane until its screen holds some text, or until it stops" +
				" changing, and give back the screen. With no text to wait for it waits for" +
				" the screen to go quiet, which is what waiting for a command to finish looks" +
				" like when the program cannot be asked. It gives back the screen either way," +
				" saying if it gave up on time.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane":       {Type: "string", Description: "which pane, from use_session_code"},
					"contains":   {Type: "string", Description: "text to wait for on the screen"},
					"quiet_ms":   {Type: "integer", Description: "how long the pane must say nothing for"},
					"timeout_ms": {Type: "integer", Description: "how long to wait before giving up"},
				},
				Required: []string{"pane"},
			},
		},
	}
}

// runTool does one tool call.
//
// A tool that ran and could not do what it was asked says so in its
// answer. A call this server cannot make sense of at all -- a tool it
// does not have, arguments it cannot read, a call that does not say
// which pane -- is a failure of the message, which is what the protocol
// asks for.
func (s *server) runTool(name string, args json.RawMessage) (result, *rpcError) {
	var in struct {
		Code      string `json:"code"`
		Pane      string `json:"pane"`
		Text      string `json:"text"`
		Contains  string `json:"contains"`
		QuietMS   int    `json:"quiet_ms"`
		TimeoutMS int    `json:"timeout_ms"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return result{}, &rpcError{
				Code:    codeInvalidParams,
				Message: "those are not arguments this tool understands",
			}
		}
	}

	switch name {
	case "use_session_code":
		if in.Code == "" {
			return missing("code")
		}
		pane, err := s.panes.Use(in.Code)
		if err != nil {
			return wrong(err.Error())
		}
		return say(fmt.Sprintf(
			"You have %s: a %dx%d screen, as pane %q.\n\n"+
				"Read it with read_pane, type into it with send_keys, and wait for it"+
				" with wait_for. The user is watching and can take it back at any moment.",
			pane.Label, pane.Cols, pane.Rows, pane.ID))

	case "list_panes":
		panes, err := s.panes.List()
		if err != nil {
			return wrong(err.Error())
		}
		if len(panes) == 0 {
			return say("The user has not handed you a pane yet." +
				" Ask them for a session code and use it with use_session_code.")
		}
		var out string
		for _, p := range panes {
			out += fmt.Sprintf("%s: %s, %dx%d\n", p.ID, p.Label, p.Cols, p.Rows)
		}
		return say(out)

	case "read_pane":
		if in.Pane == "" {
			return missing("pane")
		}
		screen, err := s.panes.Read(in.Pane)
		if err != nil {
			return wrong(err.Error())
		}
		return say(showScreen(screen, false))

	case "send_keys":
		if in.Pane == "" {
			return missing("pane")
		}
		if err := s.panes.Send(in.Pane, in.Text); err != nil {
			return wrong(err.Error())
		}
		return say("Typed. Use wait_for to see what happens: the screen has not" +
			" caught up yet.")

	case "wait_for":
		if in.Pane == "" {
			return missing("pane")
		}
		screen, gaveUp, err := s.panes.Wait(in.Pane, Until{
			Contains:  in.Contains,
			QuietMS:   in.QuietMS,
			TimeoutMS: in.TimeoutMS,
		})
		if err != nil {
			return wrong(err.Error())
		}
		return say(showScreen(screen, gaveUp))
	}
	return result{}, &rpcError{
		Code:    codeInvalidParams,
		Message: fmt.Sprintf("%q is not a tool this server has", name),
	}
}

// missing says a call left out something it had to carry.
func missing(what string) (result, *rpcError) {
	return result{}, &rpcError{
		Code:    codeInvalidParams,
		Message: "that call carried no " + what,
	}
}

// showScreen is a screen as an agent reads it, with what the screen
// itself cannot say.
func showScreen(s Screen, gaveUp bool) string {
	out := s.Screen
	var notes []string
	if gaveUp {
		notes = append(notes, "This is the screen as time ran out;"+
			" what you were waiting for has not happened.")
	}
	if s.Gone {
		notes = append(notes, "The program in this pane has finished,"+
			" so nothing more will appear and nothing will read what you type.")
	}
	for _, n := range notes {
		out += "\n\n" + n
	}
	return out
}

// say is a tool answer.
func say(text string) (result, *rpcError) {
	return result{Content: []content{{Type: "text", Text: text}}}, nil
}

// wrong is a tool answer from a tool that ran and could not do what it
// was asked.
func wrong(why string) (result, *rpcError) {
	return result{Content: []content{{Type: "text", Text: why}}, IsError: true}, nil
}
