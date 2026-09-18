package main

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/agent"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// anAgentTyping hands a pane over, lets an agent in and returns the
// pane, the pane's name to the agent and the client.
func anAgentTyping(t *testing.T, a *testApp) (*term.Terminal, string, *agent.Client) {
	t.Helper()
	pane, code, c := handedOver(t, a)
	var got agent.Pane
	offWindow(t, a, "the window to answer the agent", func() error {
		var err error
		got, err = firstOf(c.Use(code))
		return err
	})
	f := awaitModal[*ui.Form](t, a, "the hand-over dialog", byTitle[*ui.Form](paneBoxesTitle))
	pressButton(t, a, f, "Done")
	return pane, got.ID, c
}

// What the agent typed is written down, so the user who handed the pane
// over can go back over what was done in it.
func TestWhatTheAgentTypedIsWrittenDown(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, id, c := anAgentTyping(t, a)

	offWindow(t, a, "the window to answer the agent", func() error {
		return c.Send(id, "uptime\r", nil)
	})
	offWindow(t, a, "the window to answer the agent", func() error {
		return c.Send(id, "", []string{"Up", "Enter"})
	})

	log := a.typed[pane]
	if log.count() != 2 {
		t.Fatalf("the window wrote down %d sends, want 2", log.count())
	}
	if got := log.sends[0].Text; got != "uptime\r" {
		t.Errorf("the first is %q", got)
	}
	if got := strings.Join(log.sends[1].Keys, " "); got != "Up Enter" {
		t.Errorf("the second pressed %q", got)
	}
}

// A secret the user types at the agent's asking is not written down.
// The agent never sees it, and neither does the record.
func TestASecretTheUserTypesIsNotWrittenDown(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, id, c := anAgentTyping(t, a)

	// The agent types either side of the secret, so a record that held
	// nothing at all cannot pass this.
	offWindow(t, a, "the window to answer the agent", func() error {
		return c.Send(id, "sudo -v\r", nil)
	})
	a.shells[0].out <- []byte("[sudo] password for marcus: ")
	waitFor(t, a, "the pane to show the prompt", func() bool {
		return strings.Contains(paneText(pane), "sudo")
	})
	typed := make(chan bool, 1)
	go func() {
		ok, _ := c.Secret(id, "the sudo password", 10*time.Second)
		typed <- ok
	}()
	waitFor(t, a, "the pane to ask for the secret", func() bool {
		return pane.AskedForASecret()
	})
	pane.SetFocus(true)
	for _, r := range "hunter2" {
		sendKey(t, a, input.Event{Kind: input.Text, Rune: r, NormalText: true})
	}
	sendKey(t, a, press(input.KeyEnter, 0))
	waitFor(t, a, "the agent to be told the user typed", func() bool {
		select {
		case <-typed:
			return true
		default:
			return false
		}
	})

	offWindow(t, a, "the window to answer the agent", func() error {
		return c.Send(id, "id\r", nil)
	})

	// The two the agent typed are there, and the secret between them is
	// not.
	if got := a.typed[pane].count(); got != 2 {
		t.Fatalf("the record holds %d sends, want the two the agent typed", got)
	}
	// And no pane's record holds it either, whatever a later change
	// decides to write down.
	for at, log := range a.typed {
		if got := typedText(log); strings.Contains(got, "hunter2") {
			t.Errorf("the record for %v holds the user's secret:\n%s", at, got)
		}
	}
}

// The record outlives the share, so taking a pane back does not throw
// away what was done in it.
func TestTheRecordOutlivesTheShare(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, id, c := anAgentTyping(t, a)
	offWindow(t, a, "the window to answer the agent", func() error {
		return c.Send(id, "uptime\r", nil)
	})

	if err := a.forgetHandover(pane); err != nil {
		t.Fatalf("take the pane back: %v", err)
	}

	if a.typed[pane].count() != 1 {
		t.Error("taking the pane out of the share threw the record away")
	}
}

// And it goes when the pane does: there is nothing left to read it
// against.
func TestTheRecordGoesWithThePane(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, id, c := anAgentTyping(t, a)
	offWindow(t, a, "the window to answer the agent", func() error {
		return c.Send(id, "uptime\r", nil)
	})

	a.forgetPane(pane)

	if a.typed[pane].count() != 0 {
		t.Error("the record outlived the pane")
	}
}

// The dialog says what was sent, with anything invisible shown and the
// keys named.
func TestTheDialogSaysWhatWasSent(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, id, c := anAgentTyping(t, a)
	offWindow(t, a, "the window to answer the agent", func() error {
		return c.Send(id, "grep -r \"a b\"\t.\r", nil)
	})
	offWindow(t, a, "the window to answer the agent", func() error {
		return c.Send(id, "", []string{"Ctrl+C"})
	})
	a.focus(pane)

	if err := a.showTyped(); err != nil {
		t.Fatalf("show it: %v", err)
	}

	n := awaitModal(t, a, "the record", byTitle[*ui.Notice](typedTitle))
	got := n.Message()
	when := a.typed[pane].sends[0].At.Format("15:04:05")
	for _, want := range []string{
		`grep -r "a b"\t.\r`,
		"<Ctrl+C>",
		"not what the shell ran",
		// The time it was sent, which is what makes the record an
		// account rather than a list.
		when + "  ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the dialog does not say %q:\n%s", want, got)
		}
	}
	if !n.Preformatted {
		t.Error("the record is re-wrapped, which breaks the column of times")
	}
}

// A pane no agent has typed in says so rather than opening an empty
// dialog.
func TestAPaneNoAgentHasTypedInSaysSo(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)

	err := a.showTyped()

	if err == nil {
		t.Fatal("it opened a record of nothing")
	}
	if !strings.Contains(err.Error(), "no agent has typed") {
		t.Errorf("it says %q", err)
	}
}

// The oldest sends go when the record is full, and the dialog says how
// many went rather than quietly showing less than there was.
func TestTheOldestSendsGoAndTheDialogSaysSo(t *testing.T) {
	log := &typedLog{}
	for range mostTyped + 5 {
		log.add(agentSend{Text: "x"})
	}

	if got := log.count(); got != mostTyped {
		t.Errorf("it holds %d sends, want %d", got, mostTyped)
	}
	if log.dropped != 5 {
		t.Errorf("it says %d went, want 5", log.dropped)
	}
	if got := typedText(log); !strings.Contains(got, "The first 5 are no longer kept") {
		t.Error("the dialog does not say any went")
	}
}

// A lot of text goes the same way, so one huge send cannot fill the
// window's memory.
func TestALotOfTextIsCappedToo(t *testing.T) {
	log := &typedLog{}
	big := strings.Repeat("x", 64<<10)
	for range 8 {
		log.add(agentSend{Text: big})
	}

	if log.bytes > mostTypedBytes {
		t.Errorf("it holds %d bytes, want no more than %d", log.bytes, mostTypedBytes)
	}
	// Four 64 KiB sends fit in 256 KiB, so four is what it keeps: a
	// record that dropped all the way down to one would also be under
	// the cap.
	if got := log.count(); got != 4 {
		t.Errorf("it kept %d sends, want the 4 that fit", got)
	}
	if log.dropped != 4 {
		t.Errorf("it says %d went, want 4", log.dropped)
	}
	// One send larger than the whole cap is still kept: dropping it
	// would leave the record saying nothing was sent.
	only := &typedLog{}
	only.add(agentSend{Text: strings.Repeat("y", mostTypedBytes*2)})
	if only.count() != 1 {
		t.Error("a single send larger than the cap was thrown away whole")
	}
}

// Every kind of invisible character is shown, so a record of a tab or a
// return is not a blank.
func TestEveryInvisibleCharacterIsShown(t *testing.T) {
	for _, c := range []struct{ raw, want string }{
		{"\r", `\r`},
		{"\n", `\n`},
		{"\t", `\t`},
		{`\`, `\\`},
		{"\x00", `\x00`},
		{"\x1b", `\x1b`},
		{"ls -la", "ls -la"},
		{"\u00e5\u00e4\u00f6", "\u00e5\u00e4\u00f6"},
		// Above ASCII and not printable: a zero-width space would
		// otherwise be a blank in the record.
		{"\u200b", `\u200b`},
	} {
		if got := showInvisible(c.raw); got != c.want {
			t.Errorf("%q is shown as %q, want %q", c.raw, got, c.want)
		}
	}
}

// The record uses the window's clock, which stands on its own outside a
// test. A window in front of a user has none set, so reaching for it
// directly would kill every pane the first time an agent typed.
func TestTheRecordUsesAClockAWindowReallyHas(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane, id, c := anAgentTyping(t, a)
	// As a window that nobody set a clock on: what main builds.
	a.now = nil

	offWindow(t, a, "the window to answer the agent", func() error {
		return c.Send(id, "uptime\r", nil)
	})

	if a.typed[pane].count() != 1 {
		t.Fatal("the send was not written down")
	}
	if a.typed[pane].sends[0].At.IsZero() {
		t.Error("the send has no time against it")
	}
}

// The line that opens the record is on the Servers menu, whether or not
// an agent has typed yet: the menu is not rebuilt when one does.
func TestTheServersMenuOffersTheRecord(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	a.commands()
	a.refreshServers()

	// It fails the test when the line is not there, and the line takes
	// the command's own title rather than one of its own.
	if got := serverMenuLine(t, a, typedCommand); got != "" {
		t.Errorf("the line is titled %q, want the command's own title", got)
	}
	cmd, ok := a.root.Commands.Lookup(typedCommand)
	if !ok {
		t.Fatal("the command the line runs is not registered")
	}
	if cmd.Title != typedTitle+"…" {
		t.Errorf("the line reads %q", cmd.Title)
	}
}

// Key names count towards the cap too. An agent chooses how many keys a
// send names, so a record that weighed only the text could be filled
// with key presses.
func TestKeyNamesCountTowardsTheCap(t *testing.T) {
	keys := make([]string, 64)
	for i := range keys {
		keys[i] = "PageDown"
	}

	log := &typedLog{}
	log.add(agentSend{Keys: keys})

	if want := 64 * len("PageDown"); log.bytes != want {
		t.Errorf("a send of 64 keys weighs %d, want %d", log.bytes, want)
	}

	// And enough of them drops the oldest, the same as text would.
	full := &typedLog{}
	for full.dropped == 0 && full.count() < mostTyped {
		full.add(agentSend{Keys: keys})
	}
	if full.dropped == 0 {
		t.Errorf("%d sends of nothing but keys dropped none of them", full.count())
	}
}
