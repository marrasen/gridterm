package main

import "testing"

func TestZZHopClosedUnderInflightDial(t *testing.T) {
	a, s, answering := jumpApp(t)
	a.handle(ConnectTo{Saved: "inner1"})
	waitFor(t, a, "inner1", func() bool { answering(); return a.conns["inner1"] != nil })
	a.handle(ConnectTo{Saved: "inner2"})
	waitFor(t, a, "inner2 to ask", func() bool { return len(a.st.Asks) > 0 })
	a.handle(Disconnect{Machine: "inner1"})
	waitFor(t, a, "inner1 to go", func() bool { return a.conns["inner1"] == nil })
	waitFor(t, a, "hop gone", func() bool { return s.Live() == 0 })
	waitFor(t, a, "inner2 done", func() bool { answering(); return len(a.dialing) == 0 })
	if a.conns["inner2"] == nil {
		t.Fatalf("inner2 failed; notices: %+v", a.st.Notices)
	}
}
