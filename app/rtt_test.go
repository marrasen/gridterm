package app

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/remote"
)

// A ping publishes only what a card would show differently: the round
// trip to the millisecond, and whether the machine answers.
func TestAPingPublishesOnlyWhatChanges(t *testing.T) {
	a, _ := agentApp(t)
	conn := &remote.Conn{}
	m := a.machines.At("srv")
	m.Conn = conn
	a.quiet = false
	a.pinged("srv", conn, 23*time.Millisecond+100*time.Microsecond, false)
	if a.quiet || m.RTT.Milliseconds() != 23 {
		t.Fatalf("a first answer kept quiet %v, round trip %v", a.quiet, m.RTT)
	}
	a.pinged("srv", conn, 23*time.Millisecond+400*time.Microsecond, false)
	if !a.quiet {
		t.Fatal("the same millisecond was published")
	}
	a.quiet = false
	a.pinged("srv", conn, 0, true)
	if a.quiet || !m.Silent {
		t.Fatalf("a silence kept quiet %v, silent %v", a.quiet, m.Silent)
	}
	a.quiet = false
	a.pinged("srv", &remote.Conn{}, 5*time.Millisecond, false)
	if !a.quiet || !m.Silent {
		t.Fatal("an answer on a connection since replaced was kept")
	}
}
