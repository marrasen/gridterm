package view

import (
	"testing"
	"time"

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
	for typed, want := range map[string]string{"de": "sk", "prod-d": "b", "prod": "", "": "", "x": ""} {
		if got := restOfName(names, typed); got != want {
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
