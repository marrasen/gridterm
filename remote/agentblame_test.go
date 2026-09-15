package remote

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/marrasen/gridterm/internal/sshtest"
)

// noAgentHere points SSH_AUTH_SOCK at nothing, so dialling the agent
// fails the way it does on a machine with no agent running.
func noAgentHere(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "not-an-agent")
	if runtime.GOOS == "windows" {
		// Only a pipe is honoured there, and this one is not served.
		path = `\\.\pipe\gridterm-test-no-agent`
	}
	t.Setenv("SSH_AUTH_SOCK", path)
}

// An agent that is not running is not remembered as one that will not
// answer.
//
// Finding that out is opening a socket that is not there, which costs
// nothing. Remembering it meant that starting an agent after the window
// left it unused for the rest of the session.
func TestAnAgentThatIsNotThereIsNotHeldAgainstIt(t *testing.T) {
	noAgentHere(t)
	s := sshtest.New(t)
	ring := NewRing()

	cfg := testConfig(t, s)
	cfg.NoAgent = false
	cfg.Ring = ring

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if why := ring.AgentTrouble(); why != nil {
		t.Fatalf("it will not ask the agent again because %v", why)
	}
}

// A read that failed because this end closed the socket is not blamed on
// the agent.
//
// Giving up on a connection closes it, and the read the agent was in
// fails with "file already closed". The agent did nothing wrong, and a
// user who cancelled one connection was losing the agent for the rest of
// the session.
func TestOurOwnCloseIsNotBlamedOnTheAgent(t *testing.T) {
	a := &auth{agent: &shutCounter{}}
	if a.weLetGoOfTheAgent() {
		t.Fatal("it says the socket was closed from here before anything closed it")
	}
	if err := a.closeAgent(); err != nil {
		t.Fatalf("close the agent: %v", err)
	}
	if !a.weLetGoOfTheAgent() {
		t.Fatal("it does not know the socket was closed from here")
	}
}

// Waiting on a person is not the agent failing.
//
// A key on a hardware token waits for somebody to touch it. Somebody
// slow to touch one is not a broken agent, and remembering it that way
// took the agent away from every connection after it.
func TestBeingSlowToSignIsNotHeldAgainstTheAgent(t *testing.T) {
	sock := &shutCounter{}
	ring := NewRing()
	a := &auth{agent: sock, ring: ring}

	a.holdTheAgentTo(20*time.Millisecond, "to sign", "", false)
	for deadline := time.Now().Add(5 * time.Second); sock.count() == 0; {
		if time.Now().After(deadline) {
			t.Fatal("the agent socket was never closed")
		}
		time.Sleep(time.Millisecond)
	}
	if why := ring.AgentTrouble(); why != nil {
		t.Fatalf("it will not ask the agent again because %v", why)
	}

	// The listing is the half that is remembered.
	sock2 := &shutCounter{}
	b := &auth{agent: sock2, ring: ring}
	b.holdTheAgentTo(20*time.Millisecond, "to say what keys it holds", "", true)
	for deadline := time.Now().Add(5 * time.Second); ring.AgentTrouble() == nil; {
		if time.Now().After(deadline) {
			t.Fatal("an agent that would not list its keys was not remembered")
		}
		time.Sleep(time.Millisecond)
	}
}

// A watch that has already fired when it is replaced says nothing.
//
// Stopping a timer does not stop a run of it that has already started.
// One that ran anyway said the agent had not answered at the moment it
// had, closed the socket under the signing about to use it, and left the
// agent blamed for the rest of the session.
func TestAWatchThatIsAlreadyRunningWhenItIsReplacedSaysNothing(t *testing.T) {
	sock := &shutCounter{}
	ring := NewRing()
	var said []string
	a := &auth{agent: sock, ring: ring, saying: func(what string) { said = append(said, what) }}

	a.holdTheAgentTo(20*time.Millisecond, "to say what keys it holds", "", true)

	// Held here on purpose. The watch fires while this is held, and the
	// first thing it does is wait for this lock, which is exactly the
	// moment the agent answers and the watch is stopped.
	a.mu.Lock()
	time.Sleep(100 * time.Millisecond)
	a.watching++ // what done does
	a.mu.Unlock()

	time.Sleep(100 * time.Millisecond)
	if n := sock.count(); n != 0 {
		t.Errorf("the agent socket was closed %d times, want not at all", n)
	}
	if why := ring.AgentTrouble(); why != nil {
		t.Errorf("it will not ask the agent again because %v", why)
	}
	if len(said) > 0 {
		t.Errorf("it said %q about an agent that had already answered", said)
	}
}
