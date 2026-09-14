package conns

import (
	"testing"
	"time"

	"github.com/marrasen/gridterm/meter"
)

var at = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

// entry returns a connection on a machine, with a meter of its own.
func entry(host string, kind Kind, label string) *Entry {
	return &Entry{Host: host, Kind: kind, Label: label, Meter: meter.New()}
}

// hosts names the machines a set of groups covers, in order.
func hosts(groups []Group) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Host
	}
	return out
}

// labels names the rows of one group, in order.
func labels(g Group) []string {
	out := make([]string, len(g.Rows))
	for i, row := range g.Rows {
		out[i] = row.Label
	}
	return out
}

// This machine is always first and always there, so a local shell has
// somewhere to be shown even before anything else is open.
func TestGroupsAlwaysStartWithThisMachine(t *testing.T) {
	r := New()
	got := r.Groups(at)
	if len(got) != 1 || got[0].Host != Local {
		t.Fatalf("an empty registry gives %v, want this machine on its own", hosts(got))
	}
	if len(got[0].Rows) != 0 {
		t.Fatalf("%d rows on an empty registry", len(got[0].Rows))
	}
}

// A machine keeps the place its first connection gave it, so the list
// does not reorder itself while the user is looking at it.
func TestGroupsKeepTheOrderTheyWereOpenedIn(t *testing.T) {
	r := New()
	r.Add(entry("margit", Terminal, "first"))
	r.Add(entry(Local, Terminal, "a local shell"))
	r.Add(entry("web1", Terminal, "over there"))
	r.Add(entry("margit", Tunnel, "a tunnel"))

	got := r.Groups(at)
	want := []string{Local, "margit", "web1"}
	if names := hosts(got); len(names) != len(want) {
		t.Fatalf("groups = %v, want %v", names, want)
	} else {
		for i := range want {
			if names[i] != want[i] {
				t.Fatalf("groups = %v, want %v", names, want)
			}
		}
	}
	if rows := labels(got[1]); len(rows) != 2 || rows[0] != "first" || rows[1] != "a tunnel" {
		t.Fatalf("margit holds %v, want them in the order they were opened", rows)
	}
	if rows := labels(got[0]); len(rows) != 1 || rows[0] != "a local shell" {
		t.Fatalf("this machine holds %v", rows)
	}
}

// What the panel draws is worked out afresh every frame, so a connection
// falls from active to settled with no timer anywhere.
func TestRowsCarryTheStateAtTheMomentAsked(t *testing.T) {
	r := New()
	e := entry("margit", Terminal, "a build")
	r.Add(e)

	if got := r.Groups(at)[1].Rows[0].State; got != meter.Opened {
		t.Fatalf("state = %v, want opened", got)
	}
	e.Meter.Moved(128, 0, at)
	if got := r.Groups(at)[1].Rows[0].State; got != meter.Active {
		t.Fatalf("state = %v, want active", got)
	}
	// The same registry, asked about a later moment.
	later := at.Add(meter.Settle)
	if got := r.Groups(later)[1].Rows[0].State; got != meter.Settled {
		t.Fatalf("state = %v at the boundary, want settled", got)
	}
}

// A connection with nothing to count is not broken, it is just quiet.
func TestAnEntryWithNoMeterIsOpened(t *testing.T) {
	e := &Entry{Host: "margit", Kind: Tunnel, Label: "waiting"}
	if got := e.State(at); got != meter.Opened {
		t.Fatalf("state = %v, want opened", got)
	}
}

func TestAddIsIdempotentAndDropTakesOneOff(t *testing.T) {
	r := New()
	e := entry("margit", Terminal, "one")
	r.Add(e)
	r.Add(e)
	if r.Len() != 1 {
		t.Fatalf("%d entries after adding the same one twice", r.Len())
	}

	other := entry("margit", Terminal, "two")
	r.Add(other)
	r.Drop(e)
	if r.Len() != 1 {
		t.Fatalf("%d entries after dropping one of two", r.Len())
	}
	if rows := labels(r.Groups(at)[1]); len(rows) != 1 || rows[0] != "two" {
		t.Fatalf("the wrong one was dropped: %v", rows)
	}
	// Dropping something that is not there changes nothing.
	r.Drop(e)
	if r.Len() != 1 {
		t.Fatalf("%d entries after dropping one that had gone", r.Len())
	}
}

func TestAddIgnoresNothing(t *testing.T) {
	r := New()
	r.Add(nil)
	if r.Len() != 0 {
		t.Fatal("a nil entry was added")
	}
}

// A machine with nothing left open goes off the list, so the panel does
// not keep a heading for a server nobody is on.
func TestAMachineGoesWhenItsLastConnectionDoes(t *testing.T) {
	r := New()
	e := entry("margit", Terminal, "one")
	r.Add(e)
	if len(r.Groups(at)) != 2 {
		t.Fatal("the machine did not appear")
	}
	r.Drop(e)
	got := r.Groups(at)
	if len(got) != 1 || got[0].Host != Local {
		t.Fatalf("groups = %v, want this machine on its own", hosts(got))
	}
}

// What a command did after it stopped is worth reading, so a finished
// connection stays until it is dismissed.
func TestFinishedConnectionsStayUntilTheyAreCleared(t *testing.T) {
	r := New()
	running := entry("margit", Terminal, "still going")
	done := entry("margit", Command, "apt-get upgrade")
	r.Add(running)
	r.Add(done)
	done.Meter.Close()

	if rows := labels(r.Groups(at)[1]); len(rows) != 2 {
		t.Fatalf("the finished one went on its own: %v", rows)
	}

	if n := r.DropFinished(at); n != 1 {
		t.Fatalf("cleared %d, want the one that had finished", n)
	}
	if rows := labels(r.Groups(at)[1]); len(rows) != 1 || rows[0] != "still going" {
		t.Fatalf("after clearing, margit holds %v", rows)
	}
	// And clearing again has nothing to do.
	if n := r.DropFinished(at); n != 0 {
		t.Fatalf("cleared %d the second time", n)
	}
}

// Clearing must not leave what it threw away reachable through the
// slice it kept.
func TestClearingLetsGoOfWhatItDropped(t *testing.T) {
	r := New()
	for i := 0; i < 4; i++ {
		e := entry("margit", Command, "done")
		e.Meter.Close()
		r.Add(e)
	}
	r.Add(entry("margit", Terminal, "still going"))
	r.DropFinished(at)

	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.entries); i < cap(r.entries); i++ {
		if r.entries[:cap(r.entries)][i] != nil {
			t.Fatal("a cleared connection is still held by the list it was taken off")
		}
	}
}

func TestKindNames(t *testing.T) {
	want := map[Kind]string{
		Terminal: "Terminal", Command: "Command", Files: "Files", Tunnel: "Tunnel",
		Server: "Server",
	}
	for kind, name := range want {
		if got := kind.String(); got != name {
			t.Errorf("%d.String() = %q, want %q", kind, got, name)
		}
	}
	if got := Kind(99).String(); got != "Unknown" {
		t.Errorf("Kind(99).String() = %q, want Unknown", got)
	}
}

// Taking one connection off shifts the rest down and leaves the slot at
// the end holding what used to be last. An Entry keeps a whole terminal
// alive through its Reveal and Close, so that slot has to be cleared.
func TestDropLetsGoPastTheEndOfTheList(t *testing.T) {
	r := New()
	for _, label := range []string{"one", "two", "three"} {
		r.Add(entry("margit", Terminal, label))
	}
	r.Drop(r.Groups(at)[1].Rows[0].Entry)

	r.mu.Lock()
	defer r.mu.Unlock()
	held := r.entries[:cap(r.entries)]
	for i := len(r.entries); i < len(held); i++ {
		if held[i] != nil {
			t.Fatalf("the list still holds %q past its end", held[i].Label)
		}
	}
}

// The panel reads this on every frame while connections are opening and
// closing.
func TestRegistryFromSeveralGoroutines(t *testing.T) {
	r := New()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				r.Groups(time.Now())
				r.Len()
			}
		}
	}()

	for i := 0; i < 200; i++ {
		e := entry("margit", Terminal, "one")
		r.Add(e)
		r.Drop(e)
	}
	close(stop)
	<-done

	if r.Len() != 0 {
		t.Fatalf("%d entries left", r.Len())
	}
}
