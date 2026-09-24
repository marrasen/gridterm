//go:build !windows

package remote

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// An agent that takes the connection and then says nothing is given up
// on, rather than holding the window that asked.
//
// This is asked from the goroutine that draws, in the moment between
// picking a key and being warned about it, so a wait with no end is a
// window that has stopped with no way out.
func TestAgentHoldsGivesUpOnAnAgentThatSaysNothing(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "agent.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("no unix socket here: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			// Taken and never answered, which is what a wedged agent
			// looks like from here.
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()
	t.Setenv("SSH_AUTH_SOCK", sock)

	was := agentPatience
	agentPatience = 50 * time.Millisecond
	t.Cleanup(func() { agentPatience = was })

	done := make(chan error, 1)
	go func() {
		_, err := AgentHolds("SHA256:whatever")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("an agent that said nothing was reported as having answered")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("AgentHolds never came back from an agent that says nothing")
	}
}

// And one with no agent at all answers straight away.
func TestAgentHoldsWithNoAgentSaysSo(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	held, err := AgentHolds("SHA256:whatever")
	if err == nil {
		t.Error("it found an agent where there is none")
	}
	if held {
		t.Error("it says the agent holds a key where there is no agent")
	}
}
