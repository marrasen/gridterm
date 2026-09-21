package remote

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newBook returns an empty book in a directory of its own.
func newBook(t *testing.T) *Book {
	t.Helper()
	b, err := LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatalf("LoadBook: %v", err)
	}
	return b
}

// margit is a saved server to put in a book.
func margit() Host {
	return Host{Name: "margit", Address: "margit.skalarit.net", User: "marcus"}
}

// A machine that has never run gridterm has no file, and that is not
// something to report.
func TestLoadBookWithNoFileIsEmptyAndFine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "servers.json")
	b, err := LoadBook(path)
	if err != nil {
		t.Fatalf("LoadBook: %v", err)
	}
	if len(b.Hosts()) != 0 {
		t.Fatalf("%d servers, want none", len(b.Hosts()))
	}
	if b.Err() != nil {
		t.Fatalf("Err = %v, want nil", b.Err())
	}
	// And it saves, creating the directory on the way.
	if err := b.Put(margit(), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
}

func TestBookSavesAndLoadsBack(t *testing.T) {
	b := newBook(t)
	want := Host{
		Name: "web1", Address: "web1.example", Port: 2222, User: "deploy",
		Identities: []string{"/home/marcus/.ssh/id_deploy"}, Term: "xterm",
	}
	if err := b.Put(want, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	again, err := LoadBook(b.Path())
	if err != nil {
		t.Fatalf("LoadBook: %v", err)
	}
	got, ok := again.Lookup("web1")
	if !ok {
		t.Fatal("the saved server was not read back")
	}
	if got.Address != want.Address || got.Port != want.Port || got.User != want.User ||
		got.Term != want.Term || len(got.Identities) != 1 ||
		got.Identities[0] != want.Identities[0] {
		t.Fatalf("read back %+v, want %+v", got, want)
	}
}

// A file nobody could read is somebody's list of servers. Replacing it
// with an empty one loses it for good.
func TestBookThatCouldNotBeReadWillNotBeWrittenOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	const garbage = "{ this is not json"
	if err := os.WriteFile(path, []byte(garbage), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	b, err := LoadBook(path)
	if err == nil {
		t.Fatal("LoadBook read a file that is not a server list")
	}
	if b == nil {
		t.Fatal("LoadBook returned no book, so the window has nothing to work with")
	}
	if b.Err() == nil {
		t.Fatal("the book does not know it could not be read")
	}

	if err := b.Put(margit(), ""); !errors.Is(err, ErrUnsaveable) {
		t.Fatalf("Put = %v, want it to refuse", err)
	}
	if err := b.Remove("margit"); !errors.Is(err, ErrUnsaveable) {
		t.Fatalf("Remove = %v, want it to refuse", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(raw) != garbage {
		t.Fatalf("the file was changed to %q", raw)
	}
}

// A file written by a newer gridterm may hold fields this one would drop
// on the way through.
func TestBookFromANewerVersionWillNotBeWrittenOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"servers":[]}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := LoadBook(path)
	if err == nil {
		t.Fatal("LoadBook accepted a file from a newer version")
	}
	if !strings.Contains(err.Error(), "newer") {
		t.Fatalf("error = %v, want it to say the file is newer", err)
	}
	if err := b.Put(margit(), ""); !errors.Is(err, ErrUnsaveable) {
		t.Fatalf("Put = %v, want it to refuse", err)
	}
}

// A server in the file that is not a server is a file to repair, not one
// to quietly drop an entry from.
func TestBookWithABadEntryWillNotBeWrittenOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	const bad = `{"version":1,"servers":[{"name":"ok","address":"a"},{"name":"","address":"b"}]}`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := LoadBook(path)
	if err == nil {
		t.Fatal("LoadBook accepted a server with no name")
	}
	if len(b.Hosts()) != 0 {
		t.Fatalf("%d servers came back from a file that could not be read", len(b.Hosts()))
	}
}

func TestBookRefusesTwoServersWithTheSameName(t *testing.T) {
	b := newBook(t)
	if err := b.Put(margit(), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	other := Host{Name: "Margit", Address: "elsewhere.example"}
	if err := b.Put(other, ""); err == nil {
		t.Fatal("a second server took a name already in use")
	}
	if len(b.Hosts()) != 1 {
		t.Fatalf("%d servers, want the one that was there", len(b.Hosts()))
	}
}

func TestBookEditsAndRenames(t *testing.T) {
	b := newBook(t)
	if err := b.Put(margit(), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Something reached through it, so a rename has to carry it along.
	via := Host{Name: "web1", Address: "web1.internal", Via: "margit"}
	if err := b.Put(via, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	renamed := margit()
	renamed.Name = "bastion"
	renamed.User = "root"
	if err := b.Put(renamed, "margit"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if _, ok := b.Lookup("margit"); ok {
		t.Error("the old name is still there")
	}
	got, ok := b.Lookup("bastion")
	if !ok {
		t.Fatal("the new name is not there")
	}
	if got.User != "root" {
		t.Errorf("user = %q, want the edit to have stuck", got.User)
	}
	after, _ := b.Lookup("web1")
	if after.Via != "bastion" {
		t.Errorf("web1 goes through %q, want it to follow the rename", after.Via)
	}
}

// A server nothing can be reached through is one to edit, not one to
// leave pointing at nothing.
func TestBookWillNotRemoveSomethingStillReachedThrough(t *testing.T) {
	b := newBook(t)
	if err := b.Put(margit(), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := b.Put(Host{Name: "web1", Address: "web1.internal", Via: "margit"}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	err := b.Remove("margit")
	if err == nil {
		t.Fatal("a server still used as a route was removed")
	}
	if !strings.Contains(err.Error(), "web1") {
		t.Fatalf("error = %v, want it to name what needs it", err)
	}

	// The one that needs it can go, and then so can it.
	if err := b.Remove("web1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := b.Remove("margit"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(b.Hosts()) != 0 {
		t.Fatalf("%d servers left, want none", len(b.Hosts()))
	}
}

func TestBookRefusesARouteThatGoesNowhere(t *testing.T) {
	b := newBook(t)
	err := b.Put(Host{Name: "web1", Address: "web1.internal", Via: "nothing"}, "")
	if err == nil {
		t.Fatal("a server was saved with a route through something that does not exist")
	}
}

// A route that goes round in a circle would be found only when somebody
// tried to use it, by which time the dial has already started.
func TestBookRefusesARouteThatGoesRoundInACircle(t *testing.T) {
	b := newBook(t)
	if err := b.Put(Host{Name: "a", Address: "a.example"}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := b.Put(Host{Name: "b", Address: "b.example", Via: "a"}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Closing the loop: a through b, which already goes through a.
	loop := Host{Name: "a", Address: "a.example", Via: "b"}
	if err := b.Put(loop, "a"); err == nil {
		t.Fatal("a route that goes round in a circle was saved")
	}
	// And the list is as it was.
	got, _ := b.Lookup("a")
	if got.Via != "" {
		t.Fatalf("a goes through %q after the refusal", got.Via)
	}
}

func TestBookRouteListsTheHopsInOrder(t *testing.T) {
	b := newBook(t)
	for _, h := range []Host{
		{Name: "edge", Address: "edge.example"},
		{Name: "bastion", Address: "bastion.internal", Via: "edge"},
		{Name: "db", Address: "db.internal", Via: "bastion"},
	} {
		if err := b.Put(h, ""); err != nil {
			t.Fatalf("Put %s: %v", h.Name, err)
		}
	}

	route, err := b.Route("db")
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	want := []string{"edge", "bastion", "db"}
	if len(route) != len(want) {
		t.Fatalf("route is %d long, want %d", len(route), len(want))
	}
	for i, name := range want {
		if route[i].Name != name {
			t.Fatalf("route = %v, want %v", names(route), want)
		}
	}
}

func names(hosts []Host) []string {
	out := make([]string, len(hosts))
	for i, h := range hosts {
		out[i] = h.Name
	}
	return out
}

// A save that cannot land changes nothing: no file appears, and the list
// in memory is as it was rather than showing a server that is saved
// nowhere.
func TestBookSaveThatCannotLandChangesNothing(t *testing.T) {
	dir := t.TempDir()
	// A file where a directory would have to be, so nothing can be
	// created under it.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	path := filepath.Join(blocker, "sub", "servers.json")

	// What the machine says about a path under a file differs, and the
	// book is left holding nothing either way: Windows reports that
	// there is nothing there, which loads as an empty list, and Linux
	// reports "not a directory", which the book keeps as the reason it
	// cannot save. The save below must not land in either case.
	b, _ := LoadBook(path)
	if n := len(b.Hosts()); n != 0 {
		t.Fatalf("%d servers after a load that found nothing, want none", n)
	}
	if err := b.Put(margit(), ""); err == nil {
		t.Fatal("a save that could not land reported success")
	}
	if n := len(b.Hosts()); n != 0 {
		t.Fatalf("%d servers in memory after a failed save, want none", n)
	}
	// Nothing was written. Only a stat that succeeds would mean a file
	// is there: why it fails is the machine's business, and differs.
	if _, err := os.Stat(path); err == nil {
		t.Fatal("something was written anyway")
	}
}

// A change refused after it was applied leaves the list exactly as it
// was, on disk and in memory.
func TestBookRefusedChangeLeavesTheListAlone(t *testing.T) {
	b := newBook(t)
	for _, h := range []Host{
		{Name: "a", Address: "a.example"},
		{Name: "b", Address: "b.example", Via: "a"},
	} {
		if err := b.Put(h, ""); err != nil {
			t.Fatalf("Put %s: %v", h.Name, err)
		}
	}
	before, err := os.ReadFile(b.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Closing the loop, which is only found once the change is applied.
	if err := b.Put(Host{Name: "a", Address: "a.example", Via: "b"}, "a"); err == nil {
		t.Fatal("a route that goes round in a circle was saved")
	}

	got, _ := b.Lookup("a")
	if got.Via != "" {
		t.Fatalf("a goes through %q after the refusal", got.Via)
	}
	after, err := os.ReadFile(b.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("the file changed: %s", after)
	}
}

func TestHostTargetReadsBackTheWayItWasTyped(t *testing.T) {
	cases := []struct {
		host Host
		want string
	}{
		{Host{Address: "example.com"}, "example.com"},
		{Host{Address: "example.com", Port: 22}, "example.com"},
		{Host{Address: "example.com", Port: 2222}, "example.com:2222"},
		{Host{Address: "example.com", User: "marcus"}, "marcus@example.com"},
		{Host{Address: "::1", Port: 2222, User: "root"}, "root@[::1]:2222"},
		{Host{Address: "::1"}, "[::1]"},
	}
	for _, tc := range cases {
		if got := tc.host.Target(); got != tc.want {
			t.Errorf("Target() = %q, want %q", got, tc.want)
		}
	}
}

// What a target parses to has to be what a host saves, or a server saved
// from the connect dialog would reach a different machine.
func TestHostFromTargetRoundTripsThroughTarget(t *testing.T) {
	for _, target := range []string{
		"example.com", "marcus@example.com", "example.com:2222",
		"marcus@example.com:2222", "root@[::1]:22",
	} {
		h, err := HostFromTarget("", target)
		if err != nil {
			t.Fatalf("HostFromTarget(%q): %v", target, err)
		}
		again, err := ParseTarget(h.Target())
		if err != nil {
			t.Fatalf("ParseTarget(%q): %v", h.Target(), err)
		}
		first, _ := ParseTarget(target)
		if again.Host != first.Host || again.User != first.User {
			t.Errorf("%q became %q, which is %+v rather than %+v",
				target, h.Target(), again, first)
		}
	}
}

func TestHostFromTargetNamesItselfWhenNoNameWasGiven(t *testing.T) {
	h, err := HostFromTarget("  ", "marcus@margit.skalarit.net")
	if err != nil {
		t.Fatalf("HostFromTarget: %v", err)
	}
	if h.Name != "margit.skalarit.net" {
		t.Fatalf("name = %q, want the address", h.Name)
	}
}

func TestHostValidateRefusesWhatCannotBeSaved(t *testing.T) {
	cases := []struct {
		name string
		host Host
	}{
		{"no name", Host{Address: "a"}},
		{"no address", Host{Name: "a"}},
		{"a control character in the name", Host{Name: "a\x1bb", Address: "a"}},
		{"a newline in the address", Host{Name: "a", Address: "a\nb"}},
		{"a space in the address", Host{Name: "a", Address: "a b"}},
		{"a port that is not a port", Host{Name: "a", Address: "a", Port: 70000}},
		{"reached through itself", Host{Name: "a", Address: "a", Via: "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.host.Validate(); err == nil {
				t.Fatalf("Validate(%+v) = nil, want an error", tc.host)
			}
		})
	}
}

// A list that cannot be read back is never written.
//
// Every change rereads the file first, so one bad write locks the user
// out of their own servers for good: the list cannot be repaired from
// inside gridterm, because repairing it is a change.
func TestABookThatCouldNotBeReadBackIsNotWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	b, err := LoadBook(path)
	if err != nil {
		t.Fatalf("OpenBook: %v", err)
	}
	if err := b.Put(Host{Name: "one", Address: "10.0.0.1"}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Past Put's own check, which is what any later mistake would be.
	b.mu.Lock()
	b.hosts = append(b.hosts, Host{Name: "One", Address: "10.0.0.2"})
	err = b.saveLocked()
	b.mu.Unlock()
	if err == nil {
		t.Fatal("it wrote a list it will not read")
	}
	if !strings.Contains(err.Error(), "twice") {
		t.Errorf("it refused with %v, want it to say the name is used twice", err)
	}

	// And the file is still the one that was there.
	again, err := LoadBook(path)
	if err != nil {
		t.Fatalf("the list on disk is not readable: %v", err)
	}
	if got := again.Names(); len(got) != 1 || got[0] != "one" {
		t.Fatalf("the list holds %v, want the one server", got)
	}
}

// A server whose name is its address is a server, not a repeated key.
//
// The check that stops a file holding "servers" twice read the tokens
// in order and could not tell a key from a value, so a name that also
// appeared as an address looked like the same key written twice. The
// list refused to load, and every change reread it first, so the user
// was locked out by a file with nothing wrong with it.
func TestANameThatIsAlsoAnAddressLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	const list = `{
  "version": 1,
  "servers": [
    {"name": "Picard", "address": "picard.marras.net", "user": "marras"},
    {"name": "www.skalarit.net", "address": "www.skalarit.net", "port": 2222}
  ]
}`
	if err := os.WriteFile(path, []byte(list), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := LoadBook(path)
	if err != nil {
		t.Fatalf("LoadBook: %v", err)
	}
	if got := b.Names(); len(got) != 2 {
		t.Fatalf("it read %v, want both servers", got)
	}
}

// And a key really written twice is still caught.
func TestAKeyWrittenTwiceIsStillCaught(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	const list = `{
  "version": 1,
  "servers": [{"name": "one", "address": "a", "address": "b"}]
}`
	if err := os.WriteFile(path, []byte(list), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadBook(path); err == nil {
		t.Fatal("a server with two addresses was read")
	} else if !strings.Contains(err.Error(), "address") {
		t.Errorf("it refused with %v, want it to name the key", err)
	}

	// Including the one the check was written for.
	const twice = `{"version": 1, "servers": [], "servers": []}`
	if err := os.WriteFile(path, []byte(twice), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadBook(path); err == nil {
		t.Fatal("a file holding the list twice was read")
	}
}
