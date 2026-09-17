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
				" is how to read the whole of something that has scrolled past." +
				status + marked,
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
			Name:  "read_output",
			Title: "Read what the last command printed",
			Description: "What the last command in the pane printed, without the screen" +
				" around it. Use this after wait_for rather than read_pane: read_pane gives" +
				" you a rectangle of the screen, with the end of whatever ran before still" +
				" in it, and you have to work out by eye where your own output starts." +
				" The pane knows. A shell with shell integration on says where each" +
				" command's output began; without it, this is everything the pane has said" +
				" since you last typed. If it is neither -- a shell that says nothing, in a" +
				" pane you have not typed in -- this says so and you want read_pane." +
				" A full-screen program has no command output, and this says that too." +
				status + marked,
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string", Description: "which pane, from use_session_code"},
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
			Name:  "ask_for_secret",
			Title: "Ask the user to type a secret",
			Description: "Ask the user to type something into the pane without showing it" +
				" to you: a password, a passphrase, a one-time code. gridterm puts a line on" +
				" the pane saying what you asked for and who asked, the user types it there," +
				" and it goes to the program in the pane. You are told that they typed" +
				" something and never what." +
				" It is the way past a program that is waiting for a password when the user" +
				" would rather you did not have one." +
				" It waits for them, so it can take a while, and it says so if they never" +
				" type anything. Nothing else of yours is answered while it waits, so ask" +
				" when you have nothing else to do and give wait_ms if you will not wait" +
				" long. A program that echoes what is typed puts it on the screen, where you" +
				" can read it like anything else: this hides what you are told, not what the" +
				" pane shows.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string", Description: "which pane, from use_session_code"},
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
				" that ended, or the command that ran. It is the same pane, so its name does" +
				" not change and what it printed before is still above what runs now." +
				" On a pane that ran one command this runs that command again, with" +
				" whatever that does to the machine, so ask the user before you use it there." +
				" It works only if the user ticked \"Restart a closed connection\" when they" +
				" handed the pane over, and use_session_code says whether they did.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string", Description: "which pane, from use_session_code"},
				},
				Required: []string{"pane"},
			},
		},
		{
			Name:  "open_pane",
			Title: "Open another pane there",
			Description: "Open a second pane where a pane you have is: another shell on the" +
				" same machine, handed to you as it opens. The answer names it and the other" +
				" tools take that name." +
				" It does not run anything: it opens a shell, and a pane that was opened to" +
				" run one command is refused, because another pane there would read as that" +
				" command run again." +
				" It opens no connection. gridterm must already be connected to that machine," +
				" and if it is not this says so and the user is the one to connect." +
				" It works only if the user ticked \"Open another pane there\" when they" +
				" handed the pane over, and use_session_code says whether they did.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": {Type: "string",
						Description: "a pane you have, from use_session_code; the new one" +
							" opens where it is"},
				},
				Required: []string{"pane"},
			},
		},
		{
			Name:  "wait_for",
			Title: "Wait for a pane",
			Description: "Watch a pane and give back its screen once the waiting is over." +
				" Use it after send_keys, before reading again. With no contains it ends when" +
				" the command you sent finishes, or when the pane has said nothing for" +
				" quiet_ms. With contains it ends when the screen holds that text, and the" +
				" quiet is switched off; a command that finishes still ends it, because text" +
				" that has not appeared by then is not going to." +
				" contains is checked against the screen as it already is, so text that is" +
				" there when the wait begins ends it at once. Leaving quiet_ms out does not" +
				" switch the quiet off: it uses about three quarters of a second." +
				" The answer says which of those ended the waiting, and says so when the time" +
				" ran out instead." +
				" A program that keeps drawing never goes quiet: top, a progress bar, a log" +
				" being followed. For one of those give contains, or do not wait at all and" +
				" read the pane instead. It takes lines as read_pane does." +
				status + marked,
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
		pane, err := s.panes.Use(in.Code)
		if err != nil {
			return wrong(err.Error())
		}
		what := "Read it with read_pane, type into it with send_keys, and wait for it" +
			" with wait_for. The user is watching and can take it back at any moment."
		if pane.May.ReadOnly {
			what = "Read it with read_pane and wait for it with wait_for. The user handed" +
				" it over to be read: send_keys is refused. They are watching and can take" +
				" it back at any moment."
		}
		if pane.Ended {
			what = "The program in it has finished, so there is nothing left to type" +
				" into: read what it printed with read_pane."
		}
		return say(fmt.Sprintf(
			"You have %s: a %dx%d screen, as pane %q.\n\n%s%s",
			pane.Label, pane.Cols, pane.Rows, pane.ID, what, alsoAllowed(pane.May)))

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
const status = " Every screen comes with what is known about the command line. A shell" +
	" with shell integration turned on tells gridterm when each command starts and stops," +
	" and then the answer says whether one is running and what the last one exited with." +
	" A shell without it tells gridterm nothing, and the answer says so: all it has then" +
	" is whether the prompt you typed at has come back, which is a guess."

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
