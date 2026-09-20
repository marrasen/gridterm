package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/agent"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// agentWith is an agent source holding one key under a comment, for a
// test that looks for that comment at the far end.
func agentWith(t *testing.T, comment string) agentSource {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("make a key: %v", err)
	}
	ring := agent.NewKeyring()
	if err := ring.Add(agent.AddedKey{PrivateKey: priv, Comment: comment}); err != nil {
		t.Fatalf("put the key in the agent: %v", err)
	}
	return func() (io.Closer, agent.Agent, error) {
		return closerFunc(func() error { return nil }), ring, nil
	}
}

// carryingConfig connects with an agent that holds one key and asks for
// it to be carried to the far end.
func carryingConfig(t *testing.T, s *sshtest.Server, comment string) Config {
	t.Helper()
	cfg := testConfig(t, s)
	cfg.NoAgent = false
	cfg.agent = agentWith(t, comment)
	cfg.ForwardAgent = true
	return cfg
}

// A machine the user asked to carry the agent to can reach it: the
// far end opens a channel back and the keys it lists are the ones held
// here. That is the whole point of forwarding, so the test proves the
// keys arrive rather than that the request was sent.
func TestTheFarEndReachesTheAgentCarriedToIt(t *testing.T) {
	s := sshtest.New(t)

	c, err := Connect(t.Context(), carryingConfig(t, s, "the-key-held-here"))
	if err != nil {
		t.Fatalf("connect carrying the agent: %v", err)
	}
	defer func() { _ = c.Close() }()
	sh, err := c.Shell(t.Context(), ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("open a shell: %v", err)
	}
	defer func() { _ = sh.Close() }()

	keys, err := s.AgentKeys()

	if err != nil {
		t.Fatalf("the far end could not read the carried agent: %v", err)
	}
	if !slices.Contains(keys, "the-key-held-here") {
		t.Errorf("the far end sees %v, want the key held here", keys)
	}
}

// Nothing is carried unless the user asked. A connection made without
// the tick leaves the far end with no agent to open.
func TestAnAgentIsNotCarriedUnlessAsked(t *testing.T) {
	s := sshtest.New(t)

	cfg := carryingConfig(t, s, "not-for-sharing")
	cfg.ForwardAgent = false
	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = c.Close() }()
	sh, err := c.Shell(t.Context(), ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("open a shell: %v", err)
	}
	defer func() { _ = sh.Close() }()

	if got := s.AgentAsked(); got != 0 {
		t.Errorf("the far end was asked %d times to carry the agent, want none", got)
	}
}

// Every pane asks, because the request is per session. Two shells on
// one connection are two requests.
func TestEveryPaneAsksForTheAgent(t *testing.T) {
	s := sshtest.New(t)

	c, err := Connect(t.Context(), carryingConfig(t, s, "shared"))
	if err != nil {
		t.Fatalf("connect carrying the agent: %v", err)
	}
	defer func() { _ = c.Close() }()
	for range 2 {
		sh, err := c.Shell(t.Context(), ShellConfig{Cols: 80, Rows: 24})
		if err != nil {
			t.Fatalf("open a shell: %v", err)
		}
		defer func() { _ = sh.Close() }()
	}

	if got := s.AgentAsked(); got != 2 {
		t.Errorf("two panes asked %d times, want twice", got)
	}
}

// A server that will not carry the agent stops the pane rather than
// opening one without it. The user ticked the box, and a pane that
// quietly had no agent would ask for a password on the next hop with
// nothing saying why.
func TestAServerRefusingTheAgentOpensNoPane(t *testing.T) {
	s := sshtest.New(t)
	s.RefuseAgent(true)

	c, err := Connect(t.Context(), carryingConfig(t, s, "refused"))
	if err != nil {
		t.Fatalf("connect carrying the agent: %v", err)
	}
	defer func() { _ = c.Close() }()

	sh, err := c.Shell(t.Context(), ShellConfig{Cols: 80, Rows: 24})

	if err == nil {
		_ = sh.Close()
		t.Fatalf("a pane opened on a server that refused the agent")
	}
	if !strings.Contains(err.Error(), "could not be carried to") {
		t.Errorf("it says %q, want it to say the agent did not get there", err)
	}
	if !strings.Contains(err.Error(), "Turn the SSH agent off for this server") {
		t.Errorf("it says %q, want it to say what to do about it", err)
	}
}

// Asking to carry an agent that is not there fails the connection, and
// says what to do about it. Connecting anyway would leave the user
// thinking their keys went with them.
func TestCarryingNoAgentIsRefused(t *testing.T) {
	s := sshtest.New(t)

	cfg := testConfig(t, s)
	cfg.ForwardAgent = true
	cfg.agent = func() (io.Closer, agent.Agent, error) {
		return nil, nil, errors.New("no SSH agent here")
	}

	c, err := Connect(t.Context(), cfg)

	if err == nil {
		_ = c.Close()
		t.Fatalf("a connection was made carrying an agent that is not running")
	}
	if !strings.Contains(err.Error(), "cannot be carried") {
		t.Errorf("it says %q, want it to say the agent did not go", err)
	}
	if !strings.Contains(err.Error(), "Turn the SSH agent off for this server") {
		t.Errorf("it says %q, want it to say what to do about it", err)
	}
}

// Each way of having no agent says which one it was. They are fixed in
// different places, and one message for all of them sent the user to
// start an agent that was already running.
func TestWhyThereIsNoAgentToCarrySaysWhich(t *testing.T) {
	gaveUp := errors.New("it had 10s to say what keys it holds and did not")
	for what, tc := range map[string]struct {
		a    *auth
		cfg  Config
		want string
	}{
		"turned off for this connection": {
			a: &auth{}, cfg: Config{NoAgent: true},
			want: "turned off for this connection",
		},
		"given up on earlier": {
			a:    &auth{noAgent: gaveUp, agentBroke: true},
			want: "Forget unlocked keys",
		},
		"none running": {
			a:    &auth{noAgent: errors.New("SSH_AUTH_SOCK is not set")},
			want: "there is no SSH agent running here",
		},
		"let go of while connecting": {
			a:    &auth{agentGone: true},
			want: "stopped answering",
		},
	} {
		got := tc.a.noAgentToCarry(tc.cfg)

		if got == nil {
			t.Errorf("%s: no reason was given", what)
			continue
		}
		if !strings.Contains(got.Error(), tc.want) {
			t.Errorf("%s: it says %q, want it to mention %q", what, got, tc.want)
		}
	}

	// The one that used to be said for all of them, which sends the user
	// to start an agent rather than to the thing that would fix it.
	a := &auth{noAgent: gaveUp, agentBroke: true}
	if got := a.noAgentToCarry(Config{}); strings.Contains(got.Error(), "there is no SSH agent running") {
		t.Errorf("an agent given up on is reported as one that is not running: %q", got)
	}
}

// The far end signs with a carried key, which is what a second hop
// does. Listing proves the keys are visible; signing proves they work.
func TestTheFarEndSignsWithACarriedKey(t *testing.T) {
	s := sshtest.New(t)

	c, err := Connect(t.Context(), carryingConfig(t, s, "for-the-next-hop"))
	if err != nil {
		t.Fatalf("connect carrying the agent: %v", err)
	}
	defer func() { _ = c.Close() }()
	sh, err := c.Shell(t.Context(), ShellConfig{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("open a shell: %v", err)
	}
	defer func() { _ = sh.Close() }()

	sig, key, err := s.SignWithCarriedAgent([]byte("what the next hop asks to have signed"))

	if err != nil {
		t.Fatalf("sign with the carried agent: %v", err)
	}
	if err := key.Verify([]byte("what the next hop asks to have signed"), sig); err != nil {
		t.Errorf("the signature does not check out: %v", err)
	}
}

// A connection with no agent carries nothing, and says so rather than
// carrying a socket that was let go of.
func TestAnAgentLetGoOfIsNotCarried(t *testing.T) {
	a := &auth{}

	if got := a.forwardingAgent(); got != nil {
		t.Errorf("an auth with no agent offers %v to carry, want nothing", got)
	}

	a.agent, a.agentClient = closerFunc(func() error { return nil }), agent.NewKeyring()
	if a.forwardingAgent() == nil {
		t.Errorf("an agent that is open is not offered to carry")
	}

	if err := a.closeAgent(); err != nil {
		t.Fatalf("close the agent: %v", err)
	}
	if got := a.forwardingAgent(); got != nil {
		t.Errorf("an agent let go of offers %v to carry, want nothing", got)
	}
}

// A saved server carries its answer into what Connect is given, so the
// tick on the dialog is what the connection uses.
func TestASavedServerCarriesTheAgentSetting(t *testing.T) {
	h := Host{Name: "kettle", Address: "kettle.example", ForwardAgent: true}

	if !h.Config().ForwardAgent {
		t.Errorf("a server with the agent on makes a config with it off")
	}
	h.ForwardAgent = false
	if h.Config().ForwardAgent {
		t.Errorf("a server with the agent off makes a config with it on")
	}
}

// The watch marks the agent gone before it closes the socket, so a
// connection taking it over never gets one that is on its way out.
// Closing can wait on a read being torn down, and the answer must not
// depend on how long that takes.
func TestAnAgentBeingClosedIsNotHandedOver(t *testing.T) {
	closing, letGo := make(chan struct{}), make(chan struct{})
	a := &auth{ring: NewRing()}
	a.agent = closerFunc(func() error {
		close(closing)
		<-letGo
		return nil
	})
	a.agentClient = agent.NewKeyring()

	a.holdTheAgentTo(time.Millisecond, "to sign", "", false)
	<-closing

	got := a.forwardingAgent()

	close(letGo)
	if got != nil {
		t.Errorf("an agent whose socket is being closed was offered to carry")
	}
}

// A watch that was stopped does not close the agent behind the
// connection that took it over.
func TestAStoppedWatchLeavesTheAgentAlone(t *testing.T) {
	shut := false
	a := &auth{ring: NewRing()}
	a.agent = closerFunc(func() error { shut = true; return nil })
	a.agentClient = agent.NewKeyring()

	a.holdTheAgentTo(50*time.Millisecond, "to sign", "", false)
	a.done()
	time.Sleep(150 * time.Millisecond)

	if shut {
		t.Errorf("the watch closed the agent after it was stopped")
	}
	if a.forwardingAgent() == nil {
		t.Errorf("the agent is not offered to carry after the watch was stopped")
	}
}
