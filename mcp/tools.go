package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
			Description: "Open the share a session code names. A share holds one or more" +
				" panes, on whatever machines the user put in it. Call this before any" +
				" other tool: the answer lists the panes, and every other tool takes a" +
				" pane's name.",
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
			Description: "The panes in your share now, each with the name the other tools" +
				" take and the size of its screen. The user adds panes and takes them out" +
				" while you work, so call it again when you want to know what you have.",
			InputSchema: schema{Type: "object", Properties: map[string]field{}},
		},
		{
			Name:  "read_pane",
			Title: "Read a pane",
			Description: "What is on the pane's screen now, as plain text. After sending a" +
				" command use wait_for instead: a read straight afterwards shows the screen" +
				" before the command has done anything." +
				status + marked,
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane":  {Type: "string", Description: "which pane, from use_session_code or list_panes"},
					"lines": {Type: "integer", Description: linesArg},
				},
				Required: []string{"pane"},
			},
		},
		{
			Name:  "read_output",
			Title: "Read what the last command printed",
			Description: "What the last command in the pane printed, without the screen" +
				" around it. Use it after wait_for rather than read_pane. A shell with shell" +
				" integration on says where each command's output began; without it, this is" +
				" everything the pane has said since you last typed. It says so when it is" +
				" neither, and when a full-screen program has no command output at all." +
				status + marked,
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string", Description: "which pane, from use_session_code or list_panes"},
					"lines": {Type: "integer", Description: fmt.Sprintf(
						"the most lines to give back, ending at the bottom. Left out, it is"+
							" %d. A longer output gives the last %d and says so.",
						mostLines, mostLines)},
				},
				Required: []string{"pane"},
			},
		},
		{
			Name:  "send_keys",
			Title: "Type into a pane",
			Description: "Put characters into the pane exactly as given. Nothing is added:" +
				" a command needs \\r at the end, or it sits on the line unrun. keys presses" +
				" named keys instead, encoded the way the program running now asks for -- a" +
				" program this same call starts gets its keys in a later call." +
				" It does not wait: call wait_for next.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string", Description: "which pane, from use_session_code or list_panes"},
					"text": {Type: "string", Description: `what to type, with "\r" for Enter`},
					"keys": {Type: "array", Items: &items{Type: "string"},
						Description: keysArg},
				},
				Required: []string{"pane"},
			},
		},
		{
			Name:  "ask_for_secret",
			Title: "Ask the user to type a secret",
			Description: "Ask the user to type something into the pane without showing it" +
				" to you: a password, a passphrase, a one-time code. They type it on the" +
				" pane and it goes to the program there; you are told that they typed and" +
				" never what. It waits for them, and nothing else of yours is answered" +
				" while it waits. A program that echoes what is typed puts it on the" +
				" screen, where you can read it: this hides what you are told, not what" +
				" the pane shows.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string", Description: "which pane, from use_session_code or list_panes"},
					"what": {Type: "string", Description: "what to ask for, in a few words," +
						` such as "the sudo password for prod". It is shown to the user.`},
					"wait_ms": {Type: "integer", Description: fmt.Sprintf(
						"how long to wait for them, in milliseconds. Left out, it waits up"+
							" to %d minutes.", int(agent.LongestSecretWait.Minutes()))},
				},
				Required: []string{"pane", "what"},
			},
		},
		{
			Name:  "restart_pane",
			Title: "Start a pane's program again",
			Description: "Start the pane's program again after it has finished: the shell" +
				" that ended, or the command that ran. The same pane, so its name does not" +
				" change and what it printed before is still above what runs now. On a pane" +
				" that ran one command this runs that command again." +
				" It works only if the user ticked \"Restart a closed connection\", which" +
				" use_session_code and list_panes both report.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string", Description: "which pane, from use_session_code or list_panes"},
				},
				Required: []string{"pane"},
			},
		},
		{
			Name:  "open_pane",
			Title: "Open another pane there",
			Description: "Open a second pane where a pane you have is: another shell on the" +
				" same machine, handed to you as it opens. The answer names it. It runs" +
				" nothing, and it opens no connection: gridterm must already be connected" +
				" to that machine. A pane opened to run one command is refused." +
				" It works only if the user ticked \"Open another pane there\", which" +
				" use_session_code and list_panes both report.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string",
						Description: "a pane you have, from use_session_code or list_panes; the new one" +
							" opens where it is"},
				},
				Required: []string{"pane"},
			},
		},
		{
			Name:  "wait_for",
			Title: "Wait for a pane",
			Description: "Watch a pane and give back its screen once the waiting is over." +
				" Use it after send_keys. With no contains it ends when the command you" +
				" sent finishes, or when the pane has said nothing for quiet_ms, which is" +
				" about three quarters of a second unless you give another. With contains" +
				" it ends when the screen holds that text, checked against the screen as it" +
				" already is, so text that is there when the wait begins ends it at once," +
				" and the quiet is switched off, though a command that finishes still ends" +
				" it." +
				" The answer says which of those ended it, or that the time ran out." +
				" A program that keeps drawing never goes quiet -- top, a progress bar, a" +
				" log being followed -- so give contains for one of those, or read the pane" +
				" instead. It takes lines as read_pane does." +
				status + marked,
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane":     {Type: "string", Description: "which pane, from use_session_code or list_panes"},
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
		What      string   `json:"what"`
		WaitMS    int      `json:"wait_ms"`
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
		panes, err := s.panes.Use(in.Code)
		if err != nil {
			return wrong(err.Error())
		}
		return say(sharedWithYou(panes))

	case "list_panes":
		panes, err := s.panes.List()
		if err != nil && len(panes) == 0 {
			return wrong(err.Error())
		}
		if len(panes) == 0 {
			return say("You have no panes. The user has not given you a session code" +
				" yet, or they have taken every pane out of the share you used, or" +
				" the gridterm window has closed." +
				" Ask them for a code and use it with use_session_code.")
		}
		var out strings.Builder
		for _, p := range panes {
			fmt.Fprintf(&out, "%s: %s, %dx%d. %s%s\n",
				p.ID, p.Label, p.Cols, p.Rows, whatItTakes(p), alsoAllowed(p.May))
		}
		if err != nil {
			// The panes that did answer, and then what went wrong.
			out.WriteString("\nA window could not be asked, so this may not be all" +
				" of it: " + err.Error())
		}
		return say(out.String())

	case "read_pane":
		if in.Pane == "" {
			return missing("pane")
		}
		lines, clamped := atMostLines(in.Lines)
		screen, err := s.panes.Read(in.Pane, lines)
		if err != nil {
			return wrong(err.Error())
		}
		return say(showScreen(screen, Ending{}, clamped))

	case "read_output":
		if in.Pane == "" {
			return missing("pane")
		}
		most, clamped := atMostLines(in.Lines)
		if most == 0 {
			most = mostLines
		}
		screen, err := s.panes.Output(in.Pane, most)
		if err != nil {
			return wrong(err.Error())
		}
		return say(showScreen(screen, Ending{}, clamped))

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
			" caught up yet, and wait_for ends when what you sent finishes. Then" +
			" read_output for what it printed.")

	case "ask_for_secret":
		if in.Pane == "" {
			return missing("pane")
		}
		if strings.TrimSpace(in.What) == "" {
			return missing("what to ask the user for")
		}
		typed, err := s.panes.Secret(in.Pane, in.What,
			time.Duration(in.WaitMS)*time.Millisecond)
		if err != nil {
			return wrong(err.Error())
		}
		if !typed {
			return say("The user has not typed anything into the pane. They may not have" +
				" seen the line, or may not want to: read the pane to see where it is," +
				" and ask them in your own words before asking again.")
		}
		return say("The user typed something into the pane. You were not told what," +
			" and you will not be. Use wait_for to see what the program does with it.")

	case "restart_pane":
		if in.Pane == "" {
			return missing("pane")
		}
		pane, err := s.panes.Restart(in.Pane)
		if err != nil {
			return wrong(err.Error())
		}
		return say(fmt.Sprintf(
			"%s is running again, as pane %q. What it printed before is still above it,"+
				" and read_output gives you what the new run prints.",
			pane.Label, pane.ID))

	case "open_pane":
		if in.Pane == "" {
			return missing("pane")
		}
		pane, err := s.panes.Open(in.Pane)
		if err != nil {
			return wrong(err.Error())
		}
		return say(fmt.Sprintf(
			"You have a second pane on %s: a %dx%d screen, as pane %q."+
				" It is yours the same way the first one is, and the user is watching it too.",
			pane.Label, pane.Cols, pane.Rows, pane.ID))

	case "wait_for":
		if in.Pane == "" {
			return missing("pane")
		}
		lines, clamped := atMostLines(in.Lines)
		screen, ended, err := s.panes.Wait(in.Pane, lines, Until{
			Contains:  in.Contains,
			QuietMS:   in.QuietMS,
			TimeoutMS: in.TimeoutMS,
		})
		if err != nil {
			return wrong(err.Error())
		}
		return say(showScreen(screen, ended, clamped))
	}
	return result{}, &rpcError{
		Code:    codeInvalidParams,
		Message: fmt.Sprintf("%q is not a tool this server has", name),
	}
}

// sharedWithYou is what an agent is told when it uses a code: every pane
// in the share, and what each one allows.
//
// The set is not fixed. The user adds panes and takes them out while the
// agent works, so it is told to ask again rather than to hold this list
// as the answer.
func sharedWithYou(panes []Pane) string {
	if len(panes) == 0 {
		return "That code names a share with no panes in it yet." +
			" Ask the user to add one, and call list_panes to see it."
	}
	var out strings.Builder
	fmt.Fprintf(&out, "The user has shared %s with you:\n", howManyPanes(len(panes)))
	for _, p := range panes {
		fmt.Fprintf(&out, "\n%s: %s, a %dx%d screen. %s%s",
			p.ID, p.Label, p.Cols, p.Rows, whatItTakes(p), alsoAllowed(p.May))
	}
	return out.String() + "\n\nThe user adds panes and takes them out while you work," +
		" so call list_panes again when you want to know what you have." +
		" They are watching all of it and can take any of it back at any moment."
}

// howManyPanes counts the panes in a share, in words.
func howManyPanes(n int) string {
	if n == 1 {
		return "one pane"
	}
	return fmt.Sprintf("%d panes", n)
}

// whatItTakes is what an agent may do with one pane of a share.
func whatItTakes(p Pane) string {
	switch {
	case p.Ended:
		return "The program in it has finished, so there is nothing left to type into:" +
			" read what it printed with read_pane."
	case p.May.ReadOnly:
		return "Read it with read_pane and wait for it with wait_for. The user shared it" +
			" to be read: send_keys is refused."
	}
	return "Read it with read_pane, type into it with send_keys, and wait for it" +
		" with wait_for."
}

// alsoAllowed says what the user ticked for this pane, and says nothing
// when they ticked nothing.
//
// An agent is told rather than left to find out by being refused. Being
// told is not what allows it: the window decides on every call, and a
// box turned off while the agent is working takes effect at once.
func alsoAllowed(may May) string {
	var can []string
	if may.Restart {
		can = append(can, "start the program again when it has finished, with restart_pane")
	}
	if may.OpenMore {
		can = append(can, "open another pane where this one is, with open_pane")
	}
	if may.ReadBack {
		can = append(can, "read above a clear, so clearing the screen hides nothing from you")
	}
	if len(can) == 0 {
		return ""
	}
	return "\n\nThe user has also allowed you to " + strings.Join(can, "; ") +
		". They can take any of that back while you work, and then the next call fails."
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
func showScreen(s Screen, ended Ending, clamped bool) string {
	out := s.Screen
	var notes []string
	if ended.GaveUp {
		notes = append(notes, "This is the screen as time ran out;"+
			" what you were waiting for has not happened.")
	} else if ended.Because != "" {
		notes = append(notes, "The waiting ended because "+ended.Because+".")
	}
	if s.Gone {
		notes = append(notes, "The program in this pane has finished,"+
			" so nothing more will appear and nothing will read what you type.")
	}
	if note := commandNote(s); note != "" {
		notes = append(notes, note)
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

// commandNote is what the shell said about the command line, and what
// the window watched for when the shell says nothing.
//
// Which of the two this is is said plainly, because one is a report and
// the other is a guess. A full-screen program has no command line to
// report on, and a pane whose program has gone has no more to say, so
// both get nothing.
func commandNote(s Screen) string {
	if s.Alt {
		return ""
	}
	if s.Gone {
		if s.Marks && s.Running {
			return "The shell was part way through a command when the program went."
		}
		return ""
	}
	if !s.Marks {
		return watchedNote(s)
	}
	if s.Running {
		return "The shell says a command is running now, so call wait_for again rather" +
			" than acting on what is on the screen."
	}
	if !s.HasStatus {
		return "The shell says no command is running, and gave no exit status for the" +
			" last one."
	}
	whose := " That is the last command anyone ran in this pane, which may be the" +
		" user's rather than yours."
	if s.Yours {
		whose = " That is what you sent."
	}
	return fmt.Sprintf(
		"The shell says the last command finished with exit status %d.", s.Status) + whose
}

// watchedNote is what an agent is told about a pane whose shell says
// nothing about its commands.
//
// The prompt coming back is the only sign there is, and it is a guess:
// a prompt that carries the time or a branch name never comes back the
// same, and a command that prints the prompt's own text looks like one.
const noMarks = "This shell does not tell gridterm when a command starts or stops," +
	" so nothing here knows for certain whether one is running."

func watchedNote(s Screen) string {
	switch {
	case !s.Watching:
		return noMarks + " Nothing has been typed here yet through these tools," +
			" so there is no prompt to watch for. Read the screen and judge for yourself."
	case s.Back:
		return noMarks + " The prompt you last typed at is back on the screen," +
			" which usually means what you sent has finished."
	}
	return noMarks + " The prompt you last typed at has not come back," +
		" which usually means what you sent is still running."
}

// notesMarker is the line between the pane's own text and what gridterm
// has to say about it. The tools name it, so an agent knows where the
// screen ends.
const notesMarker = "-- gridterm --"

// status says what an answer carries about the command line, for the
// tools that answer with a screen.
//
// Which of the two it is is always said, because one is the shell
// reporting and the other is this window guessing from the screen.
const status = " Every screen says what is known about the command line: with shell" +
	" integration on, whether one is running and what the last one exited with; without" +
	" it, only whether the prompt has come back, which is a guess."

// marked says what the marker means, for the tools that answer with a
// screen.
var marked = fmt.Sprintf(" The screen ends at the last line reading %q; what follows is"+
	" gridterm, not the pane. The last one, because a pane can print that line itself.",
	notesMarker)

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
