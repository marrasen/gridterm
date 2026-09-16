package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/agent"
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

	// Items says what an array argument holds, and is nil for the rest.
	Items *items `json:"items,omitempty"`
}

// items is what an array argument holds.
type items struct {
	Type string `json:"type"`
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
				" gridterm and gives it to you, and it looks like gt1-<port>-<letters>." +
				" It is the only way to reach anything here. Call this before any other" +
				" tool: the answer names the pane, and every other tool takes that name.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"code": {Type: "string",
						Description: "the session code the user gave you, like gt1-<port>-<letters>"},
				},
				Required: []string{"code"},
			},
		},
		{
			Name:  "list_panes",
			Title: "List the panes you have",
			Description: "The panes the user has handed you, each with the name the other" +
				" tools take and how big its screen is. It takes no arguments. It lists" +
				" nothing else of the user's, and it is empty until a session code has" +
				" been used.",
			InputSchema: schema{Type: "object", Properties: map[string]field{}},
		},
		{
			Name:  "read_pane",
			Title: "Read a pane",
			Description: "What is on the pane's screen now, as plain text. It needs the" +
				" pane's name, from use_session_code. After sending a command, use wait_for" +
				" instead: a read taken straight afterwards shows the screen before the" +
				" command has done anything. Give lines to read more than the screen, which" +
				" is how to read the whole of something that has scrolled past." + marked,
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane":  {Type: "string", Description: "which pane, from use_session_code"},
					"lines": {Type: "integer", Description: linesArg},
				},
				Required: []string{"pane"},
			},
		},
		{
			Name:  "send_keys",
			Title: "Type into a pane",
			Description: "Put characters into the pane exactly as given, as though typed" +
				" there. Nothing is added: a command needs a carriage return (\\r) at the" +
				" end, which is Enter, or it sits on the line unrun. Control characters" +
				" work: \\u0003 is ctrl+c." +
				" keys presses named keys, after the text or instead of it. The pane encodes" +
				" each the way the program running there asks for, so a key means to it what" +
				" the same key pressed at the window would. That is the program running when" +
				" this call is made: a program the text in the same call starts has not asked" +
				" for anything yet, so keys for it go in a later call. To leave vim, send" +
				` {"keys": ["Escape"]} and then {"text": ":q!", "keys": ["Enter"]}.` +
				" It does not wait for anything to happen, so call wait_for next.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string", Description: "which pane, from use_session_code"},
					"text": {Type: "string", Description: `what to type, with "\r" for Enter`},
					"keys": {Type: "array", Items: &items{Type: "string"},
						Description: keysArg},
				},
				Required: []string{"pane"},
			},
		},
		{
			Name:  "wait_for",
			Title: "Wait for a pane",
			Description: "Watch a pane until its screen holds the text in contains, or until" +
				" it has said nothing for quiet_ms, and give back the screen. With neither" +
				" it waits for the screen to go quiet, which is what waiting for a command" +
				" to finish looks like when the program cannot be asked. When the time runs" +
				" out first it still gives back the screen, and says the time ran out." +
				" Use it after send_keys, before reading again. Text already on the screen" +
				" when the wait begins ends it at once, so to wait for a fresh prompt give" +
				" quiet_ms rather than the prompt's text." +
				" A program that keeps drawing never goes quiet: top, a progress bar, a log" +
				" being followed. For one of those give contains, or do not wait at all and" +
				" read the pane instead. It takes lines as read_pane does." + marked,
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane":     {Type: "string", Description: "which pane, from use_session_code"},
					"contains": {Type: "string", Description: "text to wait for on the screen"},
					"lines":    {Type: "integer", Description: linesArg},
					"quiet_ms": {Type: "integer",
						Description: "how long the pane must say nothing for, in milliseconds"},
					"timeout_ms": {Type: "integer",
						Description: "how long to wait before giving up, in milliseconds"},
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
		Code      string   `json:"code"`
		Pane      string   `json:"pane"`
		Text      string   `json:"text"`
		Keys      []string `json:"keys"`
		Lines     int      `json:"lines"`
		Contains  string   `json:"contains"`
		QuietMS   int      `json:"quiet_ms"`
		TimeoutMS int      `json:"timeout_ms"`
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
		what := "Read it with read_pane, type into it with send_keys, and wait for it" +
			" with wait_for. The user is watching and can take it back at any moment."
		if pane.Ended {
			what = "The program in it has finished, so there is nothing left to type" +
				" into: read what it printed with read_pane."
		}
		return say(fmt.Sprintf(
			"You have %s: a %dx%d screen, as pane %q.\n\n%s",
			pane.Label, pane.Cols, pane.Rows, pane.ID, what))

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
		lines, clamped := atMostLines(in.Lines)
		screen, err := s.panes.Read(in.Pane, lines)
		if err != nil {
			return wrong(err.Error())
		}
		return say(showScreen(screen, false, clamped))

	case "send_keys":
		if in.Pane == "" {
			return missing("pane")
		}
		if in.Text == "" && len(in.Keys) == 0 {
			return missing("text and no keys")
		}
		// Refused before anything is typed, so an agent that spelled a
		// key wrong is told which names there are and has sent nothing.
		if err := agent.CheckKeys(in.Keys); err != nil {
			return wrong(err.Error())
		}
		if err := s.panes.Send(in.Pane, in.Text, in.Keys); err != nil {
			return wrong(err.Error())
		}
		return say("Sent. Use wait_for to see what happens: the screen has not" +
			" caught up yet.")

	case "wait_for":
		if in.Pane == "" {
			return missing("pane")
		}
		lines, clamped := atMostLines(in.Lines)
		screen, gaveUp, err := s.panes.Wait(in.Pane, lines, Until{
			Contains:  in.Contains,
			QuietMS:   in.QuietMS,
			TimeoutMS: in.TimeoutMS,
		})
		if err != nil {
			return wrong(err.Error())
		}
		return say(showScreen(screen, gaveUp, clamped))
	}
	return result{}, &rpcError{
		Code:    codeInvalidParams,
		Message: fmt.Sprintf("%q is not a tool this server has", name),
	}
}

// mostLines caps how many lines one read may ask for. The window's own
// limit, because a read renders a screenful at a time under the pane's
// lock and a long one keeps that window from drawing.
const mostLines = agent.MostLines

// atMostLines is how many lines to read, bounded by what one read gives,
// and whether that bound cut the number asked for. None asked for is the
// screen.
func atMostLines(n int) (lines int, clamped bool) {
	if n <= 0 {
		return 0, false
	}
	return min(n, mostLines), n > mostLines
}

// linesArg says what the lines argument does, for both tools that take
// it.
var linesArg = fmt.Sprintf("how many lines to give back, ending at the bottom of the screen"+
	" and reaching into what has scrolled off. Left out, it is the screen. At most %d:"+
	" ask for more and you get the last %d, and the answer says so.", mostLines, mostLines)

// keysArg says what the keys argument takes, and how many names.
var keysArg = fmt.Sprintf("keys to press after the text, by name, at most %d of them."+
	" The keys are: %s", agent.MostKeys, agent.KeyNames())

// missing says a call left out something it had to carry.
func missing(what string) (result, *rpcError) {
	return result{}, &rpcError{
		Code:    codeInvalidParams,
		Message: "that call carried no " + what,
	}
}

// showScreen is a screen as an agent reads it, with what the screen
// itself cannot say.
func showScreen(s Screen, gaveUp, clamped bool) string {
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
	if clamped {
		notes = append(notes, clampNote())
	}
	// Not on the alternate screen, where the note below already says why
	// there is nothing above the screen.
	if s.All && !s.Alt {
		notes = append(notes, allThereIsNote)
	}
	if s.Note != "" {
		notes = append(notes, s.Note)
	}
	// Of the screen, because the answer may be longer than the screen
	// and a row of it is not a row of the answer.
	notes = append(notes, fmt.Sprintf("Cursor at row %d, column %d of the screen.", s.Row, s.Col))
	if s.Alt {
		notes = append(notes, fullScreenNote)
	}
	// Behind a marker, so an agent quoting the screen back or comparing
	// two reads is working on the pane's own text and not on this.
	return out + "\n\n" + notesMarker + "\n" + strings.Join(notes, "\n")
}

// notesMarker is the line between the pane's own text and what gridterm
// has to say about it. The tools name it, so an agent knows where the
// screen ends.
const notesMarker = "-- gridterm --"

// marked says what the marker means, for the tools that answer with a
// screen.
var marked = fmt.Sprintf(" The screen ends at the last line reading %q; what follows it is"+
	" gridterm talking about the pane, not the pane. Take the last one: a pane can print that"+
	" line itself, and an earlier one is the pane's own text.", notesMarker)

// allThereIsNote is what an agent is told when the pane had fewer lines
// than the read asked for.
const allThereIsNote = "That is everything the pane has kept: there is nothing above it to read."

// clampNote is what an agent is told when it asked for more lines than
// one read gives.
//
// At most, not exactly: the pane may have kept fewer, and allThereIsNote
// is what says it had.
func clampNote() string {
	return fmt.Sprintf("You asked for more lines than a read gives;"+
		" this is the last %d at most.", mostLines)
}

// fullScreenNote is what an agent is told about a pane on the alternate
// screen, where asking for more lines gives the screen and nothing else.
const fullScreenNote = "This is a full-screen program: the screen is all there is," +
	" and lines above it cannot be read."

// say is a tool answer.
func say(text string) (result, *rpcError) {
	return result{Content: []content{{Type: "text", Text: text}}}, nil
}

// wrong is a tool answer from a tool that ran and could not do what it
// was asked.
func wrong(why string) (result, *rpcError) {
	return result{Content: []content{{Type: "text", Text: why}}, IsError: true}, nil
}
