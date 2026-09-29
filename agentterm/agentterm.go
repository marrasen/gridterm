// Package agentterm is how an agent works in a terminal: reading what
// it shows, with what may be offered and what must not, and typing
// into it, keeping track of where it last typed.
package agentterm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/kakel/agent"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/ui"
	uiterm "github.com/marrasen/kakel/ui/term"
)

// Typing is where an agent last typed in a pane: the prompt it typed
// at, on line, and the commands finished by then. sent says it has
// typed at all.
type Typing struct {
	at   string
	line uint64
	done uint64
	sent bool
}

// Watching reports whether it knows the prompt the agent typed at, to
// see it come back.
func (h *Typing) Watching() bool { return h.at != "" }

// Yours reports whether a command finished since the agent typed, of
// done finished in all.
func (h *Typing) Yours(done uint64) bool { return h.sent && done > h.done }

// OutputFrom is the line the last command's output begins on.
func (h *Typing) OutputFrom(at uiterm.Reading) (uint64, string, error) {
	from, marked := at.Cmd.Output()
	mine := at.Cmd.Running || (h.sent && at.Cmd.Done > h.done)
	switch {
	case marked && (!h.sent || mine):
		return from, "This is what the last command printed, from where the shell said its output began.", nil
	case h.sent:
		return h.line, "This shell does not say where a command's output begins, so this is everything the pane has said since you last typed. It starts on the line you typed at, so the first line is the prompt with your command echoed after it.", nil
	}
	return 0, "", errors.New("nothing here knows where the last command's output began: this shell does not mark its commands, and you have typed nothing in this pane. Read the pane with read_pane instead")
}

// Mark writes down the prompt the agent is typing at, to tell
// when it comes back.
func (h *Typing) Mark(t *uiterm.Terminal) {
	read := t.ReadLines(1)
	h.sent, h.done = true, read.Cmd.Done
	if read.Alt {
		return
	}
	if h.at != "" && read.Line == h.line && strings.HasPrefix(read.Before, h.at) {
		return
	}
	h.at, h.line = read.Before, read.Line
}

// Back reports whether the prompt the agent typed at is back, on a
// later line: what it typed has finished.
func (h *Typing) Back(read uiterm.Reading) bool {
	if h.at == "" || read.Alt {
		return false
	}
	return read.Line > h.line && read.Before == h.at
}

// SecretLine is what the pane says when an agent asks for a secret.
func SecretLine(what string) string {
	// Cleaned to one plain line, cut short, with nothing in it that
	// could draw or pass itself off as the window.
	asked := strings.TrimSpace(agent.CleanSecretAsk(what))
	if asked == "" {
		asked = "something it says it cannot see"
	}
	return `-- kakel: an agent wants something typed here. The window never tells it what you type. The program in this pane gets it, so if you can see the characters as you type them, the agent can read them off the screen too. It asked for: "` + asked + `" --`
}

// TypeInto types text into a terminal and then presses keys.
func TypeInto(t *uiterm.Terminal, text string, keys []string) error {
	if err := agent.CheckKeys(keys); err != nil {
		return err
	}
	going := []byte(text)
	for _, name := range keys {
		chord, err := ui.ParseChord(name)
		if err != nil {
			return fmt.Errorf("kakel cannot press %q: %w", name, err)
		}
		press := input.Event{Kind: input.KeyPress, Key: chord.Key, Mods: chord.Mods}
		if chord.Key == input.KeySpace && chord.Mods == 0 {
			press = input.Event{Kind: input.Text, Rune: ' ', NormalText: true}
		}
		going = append(going, t.EncodeKey(press)...)
	}
	t.Send(going)
	return nil
}

// StopAtTheFloor cuts what was read at the last clear, and says so.
func StopAtTheFloor(screen string, read uiterm.Reading) (string, bool, string) {
	if read.Alt || read.Floor == 0 || read.Bottom < read.Floor {
		return screen, false, ""
	}
	below := int(read.Bottom-read.Floor) + 1
	if below > CountLines(screen) {
		return screen, false, ""
	}
	return LastLines(screen, below), true, "The pane was cleared, so the lines above the clear are not offered here. They are still in the pane, and the user can scroll up to them."
}

// LastLines is the last n lines of text.
func LastLines(text string, n int) string {
	if n <= 0 {
		return ""
	}
	from := len(text)
	for left := n; left > 0; left-- {
		cut := strings.LastIndexByte(text[:from], '\n')
		if cut < 0 {
			return text
		}
		from = cut
	}
	return text[from+1:]
}

// ImagesIn are the images on the lines lastRow and the lines-1
// above it.
func ImagesIn(on []uiterm.Picture, lastRow, lines int) []agent.Image {
	first := lastRow - lines + 1
	var keep []uiterm.Picture
	for _, p := range on {
		if p.Top <= lastRow && p.Top+p.Rows-1 >= first {
			keep = append(keep, p)
		}
	}
	return ImagesSeen(keep)
}

// ImagesSeen are the images on, as an agent is told of them.
func ImagesSeen(on []uiterm.Picture) []agent.Image {
	var out []agent.Image
	for _, p := range on {
		out = append(out, agent.Image{Top: p.Top, Rows: p.Rows, Cols: p.Cols, Width: p.Width, Height: p.Height, Wire: p.Wire})
	}
	return out
}

// TrimBlankTail is text without the blank lines at its end.
func TrimBlankTail(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	end := len(text)
	for end > 0 {
		cut := strings.LastIndexByte(text[:end], '\n')
		if strings.TrimSpace(text[cut+1:end]) != "" {
			break
		}
		end = cut
	}
	return text[:end]
}

// CountLines is how many lines text holds.
func CountLines(text string) int { return strings.Count(text, "\n") + 1 }
