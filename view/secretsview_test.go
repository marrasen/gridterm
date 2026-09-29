package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/secrets"
)

// The secrets pane narrows its list to what is typed, names a key
// passphrase's key by its whole path, and shows a key's whole
// fingerprint apart from where it is.
func TestTheSecretsPaneFindsAndShowsWholly(t *testing.T) {
	win, _, publish := windowStage(t)
	st := app.State{
		Panes: []app.Pane{{ID: "p1", Title: "Secrets", Kind: app.KindSecrets}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Secrets: app.Secrets{Exists: true, Open: true,
			Items: []app.SecretItem{{ID: "1", Name: "db", User: "admin", Kind: secrets.Password},
				{ID: "2", Name: "work key", File: "/home/me/work/id_ed25519", Kind: secrets.Password},
				{ID: "3", Name: "mail", Kind: secrets.Password}},
			Keys: []app.SecretKey{{Name: "/home/me/.ssh/id_ed25519", Note: "on this machine", Fingerprint: "SHA256:0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"}}},
	}
	publish(st)
	p := win.secrets
	if p == nil {
		t.Fatal("no secrets pane")
	}
	if row := p.table.Row("2"); row.Cells[1] != "/home/me/work/id_ed25519" {
		t.Fatalf("the key passphrase's row is %q", row.Cells)
	}
	if row := p.keys.Row("SHA256:0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"); row.Cells[1] != "on this machine" || row.Cells[2] != "SHA256:0123456789abcdefghijklmnopqrstuvwxyzABCDEFG" {
		t.Fatalf("the key's row is %q", row.Cells)
	}
	p.find.SetText("wor")
	p.show(st.Secrets, lastUI)
	if k, ok := p.table.Cursor(); !ok || k != "2" {
		t.Fatalf("finding, the cursor is on %q, %v", k, ok)
	}
}

// Typed into who a secret is for, a server's name completes.
func TestAServersNameCompletes(t *testing.T) {
	names := []string{"prod-db", "prod-web", "desk"}
	for typed, want := range map[string]string{"de": "sk", "prod-d": "b", "prod": "-", "PROD-W": "eb", "x": ""} {
		if got := restOf(true, names, typed); got != want {
			t.Errorf("%q completes with %q, want %q", typed, got, want)
		}
	}
}

// A secret copied again has its own half minute: the first copy's time
// running out does not clear the second.
func TestACopyAgainHasItsOwnTime(t *testing.T) {
	win, _, publish := windowStage(t)
	st := app.State{Notices: []app.Notice{{ID: 1, Title: "db copied", Clipboard: "hunter2", Forget: true}}}
	publish(st)
	lastWindow.Frame(20 * time.Second)
	st.Notices = append(st.Notices, app.Notice{ID: 2, Title: "db copied", Clipboard: "hunter2", Forget: true})
	publish(st)
	lastWindow.Frame(15 * time.Second)
	if got := lastUI.Clipboard(); got != "hunter2" {
		t.Fatalf("the first copy's time up, the clipboard holds %q", got)
	}
	lastWindow.Frame(20 * time.Second)
	if got := lastUI.Clipboard(); got != "" {
		t.Fatalf("the second copy's time up, the clipboard holds %q", got)
	}
	_ = win
}

// A path on this machine completes as it is typed: a file's name, and a
// folder's with its separator.
func TestAPathCompletes(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"secrets.csv", "second.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	for typed, want := range map[string]string{
		filepath.Join(dir, "secr"): "ets.csv",
		filepath.Join(dir, "se"):   "c",
		filepath.Join(dir, "k"):    "eys" + sep,
		filepath.Join(dir, "x"):    "",
		dir + sep:                  "",
	} {
		if got := restOfPath(typed); got != want {
			t.Errorf("%q completes with %q, want %q", typed, got, want)
		}
	}
}

// The New SSH Key dialog refuses a path where a key is already, while
// it is open, so what was typed is not lost to a failure after.
func TestANewKeyWhereOneIsStaysOpenSayingSo(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{})
	at := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(at, []byte("a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	win.makeKeyDialog(lastUI)
	var path *widget.TextField
	for _, c := range win.dialog.Body.(*widget.Form).Children() {
		if f, ok := c.(*widget.TextField); ok && path == nil {
			path = f
		}
	}
	path.SetText(at)
	if said := win.dialog.Check(); !strings.Contains(said, "is already there") {
		t.Fatalf("with a key there, the dialog says %q", said)
	}
	path.SetText(filepath.Join(t.TempDir(), "new_ed25519"))
	if said := win.dialog.Check(); said != "" {
		t.Fatalf("with nothing there, the dialog says %q", said)
	}
}
