package app

import (
	"context"
	"time"
)

// pingEvery is how often each machine connected to is pinged, for the
// round trip its card shows, and pingPatience how long a ping waits.
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
		}
	})
}

// pingAll pings each machine connected to, and keeps the round trip on
// its record, while the connection pinged is still its connection.
func (a *app) pingAll() {
	for _, id := range a.machines.Connected() {
		conn := a.machines.Get(id).Conn
		go func() {
			ctx, cancel := context.WithTimeout(a.ctx, pingPatience)
			defer cancel()
			rtt, err := conn.Ping(ctx)
			if err != nil {
				return
			}
			a.events <- func() {
				if m := a.machines.At(id); m.Conn == conn {
					m.RTT = rtt
				}
			}
		}()
	}
}
