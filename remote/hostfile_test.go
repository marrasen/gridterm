package remote

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// What a saved server turns into is what reaches the machine. Get it
// wrong and gridterm connects as the wrong user, on the wrong port, with
// none of the keys that were chosen — and says nothing about it.
func TestHostConfigCarriesEverythingThatReachesTheMachine(t *testing.T) {
	h := Host{
		Name: "web1", Address: "web1.internal", Port: 2222, User: "deploy",
		Identities: []string{"/keys/deploy", "/keys/spare"},
		Term:       "xterm",
	}
	cfg := h.Config()
	if cfg.Host != h.Address {
		t.Errorf("host = %q, want %q", cfg.Host, h.Address)
	}
	if cfg.Port != h.Port {
		t.Errorf("port = %d, want %d", cfg.Port, h.Port)
	}
	if cfg.User != h.User {
		t.Errorf("user = %q, want %q", cfg.User, h.User)
	}
	if !slices.Equal(cfg.Identities, h.Identities) {
		t.Errorf("identities = %v, want %v", cfg.Identities, h.Identities)
	}
	// And what the address turns into is what gets dialled.
	if got := cfg.addr(); got != "web1.internal:2222" {
		t.Errorf("addr() = %q, want web1.internal:2222", got)
	}

	sh := h.Shell(100, 40)
	if sh.Cols != 100 || sh.Rows != 40 {
		t.Errorf("shell size = %dx%d, want 100x40", sh.Cols, sh.Rows)
	}
	if sh.Term != h.Term {
		t.Errorf("term = %q, want %q", sh.Term, h.Term)
	}
}

// A saved server with nothing but an address still reaches the right
// machine, on the default port, as whoever is logged in.
func TestHostConfigLeavesTheDefaultsAlone(t *testing.T) {
	cfg := Host{Name: "a", Address: "a.example"}.Config()
	if cfg.Port != 0 {
		t.Errorf("port = %d, want 0 so the default is used", cfg.Port)
	}
	if cfg.User != "" {
		t.Errorf("user = %q, want it left for the local account", cfg.User)
	}
	if got := cfg.addr(); got != "a.example:22" {
		t.Errorf("addr() = %q, want the default port filled in", got)
	}
}

// A port typed into the dialog has to survive being saved, or the
// connection quietly goes to 22.
func TestHostFromTargetKeepsThePort(t *testing.T) {
	h, err := HostFromTarget("web1", "deploy@web1.internal:2222")
	if err != nil {
		t.Fatalf("HostFromTarget: %v", err)
	}
	if h.Port != 2222 {
		t.Fatalf("port = %d, want 2222", h.Port)
	}
	if h.User != "deploy" || h.Address != "web1.internal" {
		t.Fatalf("= %+v, want deploy on web1.internal", h)
	}
	if got := h.Config().addr(); got != "web1.internal:2222" {
		t.Fatalf("the saved server would dial %q", got)
	}
}

// The file is what another version of gridterm, and the user in an
// editor, will read. Its shape is part of what this promises.
func TestBookWritesTheShapeItPromises(t *testing.T) {
	b := newBook(t)
	h := Host{
		Name: "web1", Address: "web1.internal", Port: 2222, User: "deploy",
		Via: "", Identities: []string{"/keys/one"}, Term: "xterm",
	}
	if err := b.Put(h, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	raw, err := os.ReadFile(b.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	const want = `{
  "version": 1,
  "servers": [
    {
      "name": "web1",
      "address": "web1.internal",
      "port": 2222,
      "user": "deploy",
      "identities": [
        "/keys/one"
      ],
      "term": "xterm"
    }
  ]
}
`
	if string(raw) != want {
		t.Fatalf("the file holds:\n%s\nwant:\n%s", raw, want)
	}
}

// Nothing that could be a secret has anywhere to go, and nothing may
// grow one by accident: a new field has to be added here on purpose.
func TestBookWritesOnlyTheFieldsItIsMeantTo(t *testing.T) {
	b := newBook(t)
	h := Host{
		Name: "everything", Address: "a.example", Port: 2222, User: "u",
		Identities: []string{"/k"}, Term: "xterm",
	}
	if err := b.Put(h, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// One reached through it, so via appears too.
	if err := b.Put(Host{Name: "far", Address: "b.example", Via: "everything"}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	raw, err := os.ReadFile(b.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var file struct {
		Servers []map[string]json.RawMessage `json:"servers"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("the file this wrote is not readable: %v", err)
	}

	allowed := map[string]bool{
		"name": true, "address": true, "port": true, "user": true,
		"via": true, "identities": true, "term": true,
	}
	for _, server := range file.Servers {
		for field := range server {
			if !allowed[field] {
				t.Errorf("the saved list holds a field nobody meant to write: %q", field)
			}
		}
	}
}

// A saved list must not shuffle between runs, and the order a user reads
// is not the order the bytes sort in.
func TestBookSortsTheWayAUserReads(t *testing.T) {
	b := newBook(t)
	// ASCII puts every capital before every lowercase letter, so a
	// case-sensitive sort would give Zeta, alpha, beta.
	for _, name := range []string{"beta", "Zeta", "alpha"} {
		if err := b.Put(Host{Name: name, Address: name + ".example"}, ""); err != nil {
			t.Fatalf("Put %s: %v", name, err)
		}
	}
	want := []string{"alpha", "beta", "Zeta"}
	got := names(b.Hosts())
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// A save leaves nothing behind it. The list is written through a
// temporary file, and one left in the configuration directory is a full
// copy of somebody's servers.
func TestBookLeavesNoTemporaryFilesBehind(t *testing.T) {
	b := newBook(t)
	for _, name := range []string{"one", "two", "three"} {
		if err := b.Put(Host{Name: name, Address: name + ".example"}, ""); err != nil {
			t.Fatalf("Put %s: %v", name, err)
		}
	}
	if err := b.Remove("two"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(b.Path()))
	if err != nil {
		t.Fatalf("read the directory: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(b.Path()) {
			t.Errorf("%q was left behind", e.Name())
		}
	}
}

// Route stands on its own: it is what a connection will walk, and it
// must not hand back a list that goes nowhere even if the book is asked
// something the file could not hold.
func TestRouteRefusesWhatItCannotWalk(t *testing.T) {
	b := newBook(t)
	// Reached past the checks on purpose: this is what Route is the last
	// guard against.
	b.hosts = []Host{
		{Name: "dangling", Address: "a.example", Via: "gone"},
		{Name: "p", Address: "p.example", Via: "q"},
		{Name: "q", Address: "q.example", Via: "p"},
	}

	if _, err := b.Route("nothing"); err == nil {
		t.Error("Route walked to a server that is not saved")
	}
	if _, err := b.Route("dangling"); err == nil {
		t.Error("Route walked through a server that is not saved")
	}
	if _, err := b.Route("p"); err == nil {
		t.Error("Route walked a circle")
	} else if !strings.Contains(err.Error(), "circle") {
		t.Errorf("error = %v, want it to say the route goes round in a circle", err)
	}
}

// Everywhere else in the book treats a name as the user typed it, so
// these do too.
func TestBookMatchesNamesWhateverTheCase(t *testing.T) {
	b := newBook(t)
	if err := b.Put(Host{Name: "Bastion", Address: "b.example"}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := b.Put(Host{Name: "web1", Address: "w.example", Via: "BASTION"}, ""); err != nil {
		t.Fatalf("a route spelled in another case was refused: %v", err)
	}
	if err := b.Remove("bastion"); err == nil {
		t.Error("a server still used as a route, spelled in another case, was removed")
	}
	if _, ok := b.Lookup("BASTION"); !ok {
		t.Error("Lookup did not find a name spelled in another case")
	}
}

func TestHostValidateAcceptsAGoodHost(t *testing.T) {
	h := Host{
		Name: "web 1", Address: "web1.internal", Port: 2222, User: "deploy",
		Identities: []string{"/keys/one"}, Term: "xterm-256color",
	}
	if err := h.Validate(); err != nil {
		t.Fatalf("Validate(%+v) = %v, want nil", h, err)
	}
}

func TestHostValidateRefusesMoreThingsThatCannotBeSaved(t *testing.T) {
	cases := []struct {
		name string
		host Host
	}{
		{"a name of nothing but spaces", Host{Name: "   ", Address: "a"}},
		{"a negative port", Host{Name: "a", Address: "a", Port: -1}},
		{"a key file with a newline in it", Host{
			Name: "a", Address: "a", Identities: []string{"/keys/one\nmore"}}},
		{"a key file with no name", Host{
			Name: "a", Address: "a", Identities: []string{""}}},
		{"a terminal type with a control character", Host{
			Name: "a", Address: "a", Term: "xterm\x1b"}},
		{"an address with an at sign in it", Host{Name: "a", Address: "user@host"}},
		{"reached through itself, in another case", Host{
			Name: "Box", Address: "a", Via: "Box"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.host.Validate(); err == nil {
				t.Fatalf("Validate(%+v) = nil, want an error", tc.host)
			}
		})
	}
}

// What a user typed is trimmed on the way in, so a stray space does not
// make a different server from the one they meant.
func TestBookTrimsWhatWasTyped(t *testing.T) {
	b := newBook(t)
	h := Host{
		Name: "  margit  ", Address: " margit.skalarit.net ", User: " marcus ",
		Identities: []string{"  /keys/one  ", "   "},
	}
	if err := b.Put(h, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := b.Lookup("margit")
	if !ok {
		t.Fatal("the trimmed name is not there")
	}
	if got.Name != "margit" || got.Address != "margit.skalarit.net" || got.User != "marcus" {
		t.Fatalf("saved %+v, want it trimmed", got)
	}
	if len(got.Identities) != 1 || got.Identities[0] != "/keys/one" {
		t.Fatalf("key files = %v, want the one that had something in it", got.Identities)
	}
}
