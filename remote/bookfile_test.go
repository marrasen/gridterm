package remote

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two windows are open on the same list. Each read it once at startup,
// so a save built from that read would throw away whatever the other one
// did in between.
func TestBookDoesNotWriteOverAnotherWindowsWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	first, err := LoadBook(path)
	if err != nil {
		t.Fatalf("LoadBook: %v", err)
	}
	second, err := LoadBook(path)
	if err != nil {
		t.Fatalf("LoadBook: %v", err)
	}

	if err := second.Put(Host{Name: "staging", Address: "staging.example"}, ""); err != nil {
		t.Fatalf("the second window: %v", err)
	}
	// The first window has not looked at the file since it opened.
	if err := first.Put(Host{Name: "dev", Address: "dev.example"}, ""); err != nil {
		t.Fatalf("the first window: %v", err)
	}

	again, err := LoadBook(path)
	if err != nil {
		t.Fatalf("LoadBook: %v", err)
	}
	got := names(again.Hosts())
	want := []string{"dev", "staging"}
	if len(got) != len(want) {
		t.Fatalf("the list holds %v, want %v: one window of work was lost", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the list holds %v, want %v", got, want)
		}
	}
}

// A field this build does not know about belongs to somebody. Writing
// the file back without it would delete it, so the file is reported as
// unreadable instead and left alone.
func TestBookWillNotDropAFieldItDoesNotKnow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	const extra = `{"version":1,"servers":[{"name":"a","address":"a.example","comment":"work vpn"}]}`
	if err := os.WriteFile(path, []byte(extra), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	b, err := LoadBook(path)
	if err == nil {
		t.Fatal("a file holding something this build would drop was read as if it were safe")
	}
	if err := b.Put(margit(), ""); !errors.Is(err, ErrUnsaveable) {
		t.Fatalf("Put = %v, want it to refuse", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(raw) != extra {
		t.Fatalf("the file was changed to %q", raw)
	}
}

// The decoder keeps the last of a repeated key, so a file holding
// "servers" twice would quietly become whichever came second.
func TestBookWillNotGuessAtARepeatedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	const twice = `{"version":1,"servers":[{"name":"a","address":"a.example"}],"servers":[]}`
	if err := os.WriteFile(path, []byte(twice), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := LoadBook(path)
	if err == nil {
		t.Fatal("a file holding the same key twice was read as if it were one list")
	}
	if !strings.Contains(err.Error(), "twice") {
		t.Fatalf("error = %v, want it to say what is in it twice", err)
	}
	if err := b.Put(margit(), ""); !errors.Is(err, ErrUnsaveable) {
		t.Fatalf("Put = %v, want it to refuse", err)
	}
}

// A list from a newer gridterm says so, even when that gridterm also
// added a field this build does not know.
//
// The strict decode used to run first, so the user was told "json:
// unknown field" about a file whose real trouble is that it belongs to a
// later version.
func TestANewerListSaysSoRatherThanNamingItsNewField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	const newer = `{"version":2,"servers":[],"colour":"green"}`
	if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadBook(path)

	if err == nil {
		t.Fatal("a list from a newer gridterm loaded clean")
	}
	if !strings.Contains(err.Error(), "newer gridterm") {
		t.Fatalf("error = %v, want it to say the list is from a newer gridterm", err)
	}
}

// A file that is JSON but not a server list is not a first run. Treating
// it as one is the single reading that licenses overwriting it.
func TestBookWillNotTreatNonsenseAsAFirstRun(t *testing.T) {
	for _, content := range []string{
		"null", "{}", `{"servers":[]}`, `{"version":0,"servers":[]}`,
		`{"version":-3,"servers":[]}`,
	} {
		t.Run(content, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "servers.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			b, err := LoadBook(path)
			if err == nil {
				t.Fatalf("%q was read as an empty list", content)
			}
			if err := b.Put(margit(), ""); !errors.Is(err, ErrUnsaveable) {
				t.Fatalf("Put = %v, want it to refuse", err)
			}
			raw, _ := os.ReadFile(path)
			if string(raw) != content {
				t.Fatalf("the file was changed to %q", raw)
			}
		})
	}
}

// A file with a route that goes nowhere loads clean and then refuses
// every later change for a reason the user never touched. It has to be
// reported when it is read.
func TestBookReportsABrokenRouteWhenItIsRead(t *testing.T) {
	for _, content := range []string{
		`{"version":1,"servers":[{"name":"a","address":"a.example","via":"gone"}]}`,
		`{"version":1,"servers":[` +
			`{"name":"p","address":"p.example","via":"q"},` +
			`{"name":"q","address":"q.example","via":"p"}]}`,
	} {
		path := filepath.Join(t.TempDir(), "servers.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		b, err := LoadBook(path)
		if err == nil {
			t.Fatalf("a list with a broken route was read as if it were sound: %s", content)
		}
		if b.Err() == nil {
			t.Fatal("the book does not know its routes are broken")
		}
	}
}

// A rename can take away the very name a route was pointing at. Checked
// before the change, that is approved and written to disk.
func TestBookRefusesARenameThatBreaksItsOwnRoute(t *testing.T) {
	b := newBook(t)
	if err := b.Put(Host{Name: "b", Address: "b.example"}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Rename b to c, and in the same change point c through b.
	err := b.Put(Host{Name: "c", Address: "b.example", Via: "b"}, "b")
	if err == nil {
		t.Fatal("a rename that left its own route pointing at nothing was saved")
	}
	if _, err := b.Route("b"); err != nil {
		t.Fatalf("the list was changed anyway: %v", err)
	}
}

// A config file linked in from somewhere else is a normal way to keep
// one. A rename replaces the link rather than what it points at, which
// detaches it silently and stops the real file being updated.
func TestBookSavesThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if err := os.WriteFile(real, []byte(`{"version":1,"servers":[]}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(dir, "servers.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	b, err := LoadBook(link)
	if err != nil {
		t.Fatalf("LoadBook: %v", err)
	}
	if err := b.Put(margit(), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced by a file, so the real one stopped being updated")
	}
	raw, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), "margit") {
		t.Fatalf("the real file was not updated: %s", raw)
	}
}

// What Hosts hands out must not be a way into what is saved.
func TestBookHostsCannotBeChangedFromOutside(t *testing.T) {
	const mine = "/home/marcus/.ssh/id_ed25519"
	b := newBook(t)
	h := margit()
	h.Identities = []string{mine}
	if err := b.Put(h, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got := b.Hosts()
	got[0].Identities[0] = "/tmp/somebody-else"
	if after := b.Hosts(); after[0].Identities[0] != mine {
		t.Fatalf("the book now holds %q, changed without being saved", after[0].Identities[0])
	}

	// The same through Lookup, and through what was handed to Put.
	found, _ := b.Lookup("margit")
	found.Identities[0] = "/tmp/somebody-else"
	h.Identities[0] = "/tmp/somebody-else"
	if after, _ := b.Lookup("margit"); after.Identities[0] != mine {
		t.Fatalf("the book now holds %q", after.Identities[0])
	}
}

// More than one key file survives being saved and read back.
func TestBookKeepsEveryKeyFile(t *testing.T) {
	b := newBook(t)
	h := margit()
	h.Identities = []string{"/keys/one", "/keys/two", "/keys/three"}
	if err := b.Put(h, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	again, err := LoadBook(b.Path())
	if err != nil {
		t.Fatalf("LoadBook: %v", err)
	}
	got, _ := again.Lookup("margit")
	if len(got.Identities) != 3 {
		t.Fatalf("read back %v, want all three", got.Identities)
	}
}

func TestBookPutUnderANameThatIsNotThere(t *testing.T) {
	b := newBook(t)
	if err := b.Put(margit(), "nothing"); err == nil {
		t.Fatal("a server was edited under a name that is not saved")
	}
}

// Two names that reduce to the same command id would leave one of the
// two unreachable from the menu and the palette.
func TestBookRefusesTwoNamesThatCannotBeToldApart(t *testing.T) {
	b := newBook(t)
	if err := b.Put(Host{Name: "My Box", Address: "one.example"}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	err := b.Put(Host{Name: "my-box", Address: "two.example"}, "")
	if err == nil {
		t.Fatal("two names that reduce to the same command were both saved")
	}
	if !strings.Contains(err.Error(), "My Box") {
		t.Fatalf("error = %v, want it to name the one already saved", err)
	}
}

// A book that does not know where its file lives says so, or a window
// offers to save servers it has nowhere to put.
func TestUnusableBookSaysWhy(t *testing.T) {
	want := errors.New("no configuration directory")
	b := UnusableBook(want)
	if !errors.Is(b.Err(), want) {
		t.Fatalf("Err = %v, want the reason it was built with", b.Err())
	}
	if err := b.Put(margit(), ""); !errors.Is(err, ErrUnsaveable) {
		t.Fatalf("Put = %v, want it to refuse", err)
	}
	if len(b.Hosts()) != 0 {
		t.Fatal("an unusable book claimed to hold servers")
	}
}

// The user repairs the file and says so, rather than restarting.
func TestBookReloadPicksUpARepairedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	if err := os.WriteFile(path, []byte("{ broken"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, _ := LoadBook(path)
	if b.Err() == nil {
		t.Fatal("a broken file was read as if it were sound")
	}

	const repaired = `{"version":1,"servers":[{"name":"a","address":"a.example"}]}`
	if err := os.WriteFile(path, []byte(repaired), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := b.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if b.Err() != nil {
		t.Fatalf("Err = %v after a repair", b.Err())
	}
	if got := names(b.Hosts()); len(got) != 1 || got[0] != "a" {
		t.Fatalf("the book holds %v after a repair", got)
	}
	if err := b.Put(margit(), ""); err != nil {
		t.Fatalf("Put after a repair: %v", err)
	}
}

// Two goroutines adding at once both end up in the list.
func TestBookPutFromTwoGoroutines(t *testing.T) {
	b := newBook(t)
	done := make(chan error, 2)
	for _, name := range []string{"one", "two"} {
		go func() {
			done <- b.Put(Host{Name: name, Address: name + ".example"}, "")
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if got := names(b.Hosts()); len(got) != 2 {
		t.Fatalf("the book holds %v, want both", got)
	}
}

// Two saved windows serving at one address are not a list to guess at.
//
// A window is one connection, held under the name the list gives its
// address. Two names for one address would leave one of them naming a
// connection it cannot reach.
func TestBookRefusesTwoWindowsAtOneAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	const both = `{"version":1,"servers":[` +
		`{"name":"office","address":"10.0.0.5","port":2222,"window":true},` +
		`{"name":"spare","address":"10.0.0.5","port":2222,"window":true}]}`
	if err := os.WriteFile(path, []byte(both), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	b, err := LoadBook(path)
	if err == nil {
		t.Fatal("it loaded a list that saves one window twice")
	}
	for _, want := range []string{"office", "spare", "10.0.0.5:2222"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not name %q: %v", want, err)
		}
	}
	if b.Err() == nil {
		t.Error("the book does not keep the reason it is unusable")
	}
	// And nothing is repaired behind the user's back.
	if err := b.Put(Host{Name: "dev", Address: "dev.example"}, ""); err == nil {
		t.Error("it wrote over a list it could not read")
	}
}

// A machine and a window can share an address: one is logged in to and
// the other is taken over, and neither is the other's connection.
func TestBookAllowsAMachineAtAWindowsAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	b, err := LoadBook(path)
	if err != nil {
		t.Fatalf("LoadBook: %v", err)
	}
	if err := b.Put(Host{Name: "office", Address: "10.0.0.5", Port: 2222, Window: true}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}
	if err := b.Put(Host{Name: "box", Address: "10.0.0.5", Port: 2222}, ""); err != nil {
		t.Fatalf("save the machine at the same address: %v", err)
	}
}
