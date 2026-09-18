package ui

import (
	"cmp"
	"fmt"
	"slices"
)

// Command is one thing the user can ask for.
//
// Every action gets a name here rather than only a key binding, so the
// key bindings, the command palette and the menus are three ways into
// one list. Adding a command makes it reachable from all of them, and
// they cannot drift apart.
type Command struct {
	// ID names the command for bindings, menus and settings files, such
	// as "font.increase". It is what a saved key binding refers to, so
	// changing one breaks whatever pointed at it.
	ID string

	// Title is the line a person reads in a menu or the palette.
	Title string

	// AlsoFind are other words this command answers to in the palette.
	// The title is what a person reads, and it is not always the word
	// they would look for: the colour schemes are found under "theme"
	// as well, which the title never says.
	//
	// Matching one of these never beats matching the title, so the
	// order the palette shows is the order the titles give.
	AlsoFind []string

	// Run does the work.
	Run func() error
}

// Commands is the registry every way of invoking something goes through.
//
// The set changes while the program runs: a pane registers its
// own commands when it opens and takes them away when it closes, so
// Unregister is as ordinary as Register.
type Commands struct {
	byID map[string]Command
}

// NewCommands returns an empty registry.
func NewCommands() *Commands {
	return &Commands{byID: make(map[string]Command)}
}

// Register adds a command. It fails on a duplicate id rather than
// replacing the command already registered, because the two would be
// the same line in a menu and only one of them would ever run.
func (c *Commands) Register(cmd Command) error {
	switch {
	case cmd.ID == "":
		return fmt.Errorf("command has no id")
	case cmd.Title == "":
		return fmt.Errorf("command %q has no title", cmd.ID)
	case cmd.Run == nil:
		return fmt.Errorf("command %q does nothing", cmd.ID)
	}
	if _, taken := c.byID[cmd.ID]; taken {
		return fmt.Errorf("command %q is already registered", cmd.ID)
	}
	c.byID[cmd.ID] = cmd
	return nil
}

// MustRegister adds commands and panics if any is rejected. It is for
// the fixed set a program registers at startup, where a bad command is
// a programming mistake rather than something to recover from.
func (c *Commands) MustRegister(cmds ...Command) {
	for _, cmd := range cmds {
		if err := c.Register(cmd); err != nil {
			panic(err)
		}
	}
}

// Unregister removes a command. It reports whether one was there, so a
// caller cleaning up after a closed pane can tell a double removal from
// a successful one.
func (c *Commands) Unregister(id string) bool {
	if _, had := c.byID[id]; !had {
		return false
	}
	delete(c.byID, id)
	return true
}

// Lookup returns the command with the given id.
func (c *Commands) Lookup(id string) (Command, bool) {
	cmd, ok := c.byID[id]
	return cmd, ok
}

// Run invokes a command by id, returning what it returned. An unknown
// id is an error: it means a binding or a menu points at nothing.
func (c *Commands) Run(id string) error {
	cmd, ok := c.byID[id]
	if !ok {
		return fmt.Errorf("no such command: %q", id)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command %q: %w", id, err)
	}
	return nil
}

// All returns every command, by title and then by id, so a menu and a
// palette list them in the same order every time.
func (c *Commands) All() []Command {
	out := make([]Command, 0, len(c.byID))
	for _, cmd := range c.byID {
		out = append(out, cmd)
	}
	slices.SortFunc(out, func(a, b Command) int {
		if n := cmp.Compare(a.Title, b.Title); n != 0 {
			return n
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out
}

// Len returns how many commands are registered.
func (c *Commands) Len() int { return len(c.byID) }
