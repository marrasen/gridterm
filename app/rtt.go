package app

import (
	"context"
	"time"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
)

// pingEvery is how often each machine connected to is pinged, for the
// round trip its card shows, and pingPatience how long a ping waits
// before the machine is taken as not answering.
const (
	pingEvery    = 5 * time.Second
	pingPatience = 3 * time.Second
)

// startPings pings the machines connected to now, and again every
// pingEvery while kakel runs.
func (a *app) startPings() {
	time.AfterFunc(pingEvery, func() {
		a.events <- func() {
			if a.gone {
				return
			}
			a.pingAll()
			a.startPings()
			// Asking changes nothing shown: an answer may.
			a.quiet = true
		}
	})
}

// pingAll pings each machine connected to that has no ping on its way
// already. A ping not answered in pingPatience marks the machine silent
// at once; the next waits until the server answers that one, as later
// requests queue behind it. The round trip is kept on the machine's
// record while the connection pinged is still its connection, and what
// is published changes only as what its card shows does: the round
// trip to the millisecond, and whether it answers.
func (a *app) pingAll() {
	for _, id := range a.machines.Connected() {
		m := a.machines.At(id)
		if m.Pinging {
			continue
		}
		m.Pinging = true
		conn := m.Conn
		type answer struct {
			rtt time.Duration
			err error
		}
		got := make(chan answer, 1)
		go func() {
			rtt, err := conn.Ping(context.Background())
			got <- answer{rtt, err}
		}()
		go func() {
			var ans answer
			select {
			case ans = <-got:
			case <-time.After(pingPatience):
				a.events <- func() { a.pinged(id, conn, 0, true) }
				ans = <-got
			}
			a.events <- func() {
				if m := a.machines.At(id); m.Conn == conn {
					m.Pinging = false
				}
				a.pinged(id, conn, ans.rtt, ans.err != nil)
			}
		}()
	}
}

// pinged keeps what a ping found on id's record, while conn is still
// its connection, and publishes only what a card would show differently.
func (a *app) pinged(id machines.ID, conn *remote.Conn, rtt time.Duration, silent bool) {
	m := a.machines.At(id)
	if m.Conn != conn {
		a.quiet = true
		return
	}
	changed := silent != m.Silent || !silent && rtt.Milliseconds() != m.RTT.Milliseconds()
	m.Silent = silent
	if !silent {
		m.RTT = rtt
	}
	if !changed {
		a.quiet = true
	}
}
