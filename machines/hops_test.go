package machines

import (
	"testing"

	"github.com/marrasen/kakel/remote"
)

// A jump host connected to only to go through is taken as the far end
// when it was dialled just as the far end asks now, with the agent or
// without it, and not otherwise.
func TestAHopIsTakenAsTheFarEndOnlyWithTheSameAgent(t *testing.T) {
	for _, c := range []struct {
		hopAgent, wantAgent, taken bool
	}{
		{false, false, true},
		{true, true, true},
		{true, false, false},
		{false, true, false},
	} {
		r := New(func() *remote.Book { return nil })
		bastion := remote.Config{Host: "bastion.example", Port: 22, User: "me", ForwardAgent: c.hopAgent}
		conn := &remote.Conn{}
		r.hops["bastion"] = conn
		r.routes[conn] = RouteOf([]remote.Config{bastion})
		bastion.ForwardAgent = c.wantAgent
		got, next := r.HopConnected([]ID{"bastion"}, []remote.Config{bastion})
		if (got == conn) != c.taken || (c.taken && next != 1) {
			t.Errorf("a hop dialled with the agent %v, wanted with it %v: taken %v, want %v", c.hopAgent, c.wantAgent, got == conn, c.taken)
		}
	}
}
