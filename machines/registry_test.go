package machines

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/kakel/logs"
	"github.com/marrasen/kakel/remote"
)

// withDesk is a registry whose server list holds desk, and desk's ID.
func withDesk(t *testing.T) (*Registry, ID) {
	t.Helper()
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "desk", Address: "desk.example"}, ""); err != nil {
		t.Fatal(err)
	}
	h, _ := book.Lookup("desk")
	return New(func() *remote.Book { return book }), ID(h.ID)
}

func TestMachinesAreNamedAndFoundByID(t *testing.T) {
	r, desk := withDesk(t)
	quick := r.NewQuick("me@typed.example", false)
	if again := r.NewQuick("me@typed.example", false); again != quick || !strings.HasPrefix(string(quick), "quick-") {
		t.Fatalf("a quick connection is %q, and again %q", quick, again)
	}
	far := FarID(desk, "k1")
	r.NameFar(desk, "k1", "db")
	for id, want := range map[ID]string{Local: "this computer", desk: "desk", quick: "me@typed.example", far: "db through desk", FarID(desk, "k2"): "k2 through desk"} {
		if got := r.Name(id); got != want {
			t.Errorf("%q is called %q, want %q", id, got, want)
		}
	}
	// Names become IDs, and IDs stay themselves.
	for name, want := range map[string]ID{"": Local, "This Computer": Local, "desk": desk, string(desk): desk, "me@typed.example": quick, string(quick): quick} {
		if got, ok := r.Find(name); !ok || got != want {
			t.Errorf("%q finds %q, %v, want %q", name, got, ok, want)
		}
	}
	if _, ok := r.Find("nowhere"); ok {
		t.Error("a name nothing has was found")
	}
	if window, key, ok := far.Far(); !ok || window != desk || key != "k1" || !far.Of(desk) || desk.Of(far) {
		t.Errorf("%q splits into %q, %q, %v", far, window, key, ok)
	}
}

// Forget keeps what something still uses, and lets the rest go with
// its log.
func TestForgetLetsGoOfWhatNothingUses(t *testing.T) {
	r, desk := withDesk(t)
	kept := r.NewQuick("kept.example", false)
	idle := r.NewQuick("idle.example", false)
	dropped := r.NewQuick("dropped.example", false)
	r.At(idle).Log = logs.New(0, nil)
	r.At(dropped).Dropped = true
	r.Removed("old", "old server")
	r.NameFar(desk, "k1", "db")
	inUse := map[ID]bool{kept: true}
	gone := r.Forget(func(id ID) bool { return inUse[id] })
	if len(gone) != 2 || r.IsQuick(idle) || r.Get(idle).Log != nil {
		t.Fatalf("forgot %v; idle is quick %v, with log %v", gone, r.IsQuick(idle), r.Get(idle).Log)
	}
	if !r.IsQuick(kept) || !r.IsQuick(dropped) {
		t.Fatal("forgot a quick connection still used")
	}
	if r.Name("old") != "old" || r.Name(FarID(desk, "k1")) != "k1 through desk" {
		t.Fatalf("still named %q and %q", r.Name("old"), r.Name(FarID(desk, "k1")))
	}
	if _, ok := r.all[idle]; ok {
		t.Fatal("an empty record stays")
	}
}

// A machine beyond a connected window keeps its name while the window
// is connected.
func TestAFarNameLastsWhileItsWindowIsConnected(t *testing.T) {
	r, desk := withDesk(t)
	r.At(desk).Window = &Window{}
	r.NameFar(desk, "k1", "db")
	r.Forget(func(ID) bool { return false })
	if got := r.Name(FarID(desk, "k1")); got != "db through desk" {
		t.Fatalf("with its window connected, it is called %q", got)
	}
}

// Holding a connection counts its uses, and letting go counts them off
// until none are kept. One the user connected to stays open.
func TestHopUsesAreCountedAndLetGo(t *testing.T) {
	r, desk := withDesk(t)
	c := &remote.Conn{}
	r.At(desk).Conn = c
	r.Hold(c)
	r.Hold(c)
	if r.users[c] != 2 {
		t.Fatalf("held twice, it counts %d", r.users[c])
	}
	r.Release(c)
	r.Release(c)
	if len(r.users) != 0 || len(r.routes) != 0 || len(r.hops) != 0 {
		t.Fatalf("let go, it keeps users %v, routes %v, hops %v", r.users, r.routes, r.hops)
	}
}
