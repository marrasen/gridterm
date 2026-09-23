package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/ui"
)

// aWindowWithASecretsPane is a window whose vault holds a couple of
// things, with the pane open on it.
func aWindowWithASecretsPane(t *testing.T) (*testApp, *secrets.Vault, *secretsPane) {
	t.Helper()
	a, keyFile := aWindowWithSecrets(t)
	v := startTheVault(t, a, keyFile)
	if _, err := v.Put(secrets.Item{Name: "margit", User: "kettle"}, "hunter2"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := v.Put(secrets.Item{Name: "a note", Kind: secrets.Note}, "written down"); err != nil {
		t.Fatalf("put a note: %v", err)
	}
	if err := a.showSecretsPane(); err != nil {
		t.Fatalf("open the pane: %v", err)
	}
	if a.secretsPane == nil {
		t.Fatal("no pane opened")
	}
	return a, v, a.secretsPane
}

// paneText is what the pane draws, one string per row.
func secretsPaneText(t *testing.T, p *secretsPane, cols, rows int) []string {
	t.Helper()
	g := grid.New(cols, rows, p.app.colours.FG, p.app.colours.BG)
	p.Layout(ui.Size{Cols: cols, Rows: rows})
	p.Draw(g.View())
	out := make([]string, rows)
	for y := range rows {
		var b strings.Builder
		for x := range cols {
			r := g.At(x, y).Rune
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		out[y] = strings.TrimRight(b.String(), " ")
	}
	return out
}

// The pane lists what is in the vault, and never a secret itself.
func TestTheSecretsPaneListsWhatIsInTheVault(t *testing.T) {
	_, _, p := aWindowWithASecretsPane(t)

	said := strings.Join(secretsPaneText(t, p, 80, 24), "\n")
	for _, want := range []string{manageSecretsTitle, "margit", "kettle", "a note"} {
		if !strings.Contains(said, want) {
			t.Errorf("the pane does not say %q:\n%s", want, said)
		}
	}
	// The values are what a vault is for. They are never drawn.
	for _, never := range []string{"hunter2", "written down"} {
		if strings.Contains(said, never) {
			t.Errorf("the pane drew the secret %q:\n%s", never, said)
		}
	}
}

// And the keys that open it, under a heading of their own.
func TestTheSecretsPaneListsTheKeys(t *testing.T) {
	a, _, p := aWindowWithASecretsPane(t)

	said := strings.Join(secretsPaneText(t, p, 100, 24), "\n")
	if !strings.Contains(said, secretsPaneKeys) {
		t.Errorf("the pane does not head the keys:\n%s", said)
	}
	if !strings.Contains(said, "on this machine") {
		t.Errorf("the pane does not say the key is here:\n%s", said)
	}
	_ = a
}

// A locked vault is a pane with nothing in it, names included: locking
// the keys is the user saying they want none of it on screen.
func TestTheSecretsPaneEmptiesWhenTheVaultLocks(t *testing.T) {
	a, _, p := aWindowWithASecretsPane(t)
	if said := strings.Join(secretsPaneText(t, p, 80, 24), "\n"); !strings.Contains(said, "margit") {
		t.Fatalf("the pane is empty before the lock:\n%s", said)
	}

	if err := a.lockKeys(); err != nil {
		t.Fatalf("lock: %v", err)
	}

	said := strings.Join(secretsPaneText(t, p, 80, 24), "\n")
	if strings.Contains(said, "margit") {
		t.Errorf("a locked pane still names what is in the vault:\n%s", said)
	}
	if !strings.Contains(said, "Locked") {
		t.Errorf("a locked pane does not say so:\n%s", said)
	}
	// And it is holding none of it either.
	if len(p.items) != 0 || len(p.keys) != 0 {
		t.Errorf("the pane still holds %d items and %d keys", len(p.items), len(p.keys))
	}
}

// The bar walks the list and stops at each end.
func TestTheSecretsPaneBarStopsAtTheEnds(t *testing.T) {
	_, _, p := aWindowWithASecretsPane(t)
	secretsPaneText(t, p, 80, 24)
	if p.rows() < 2 {
		t.Fatalf("the pane has %d rows, want the secrets and a key", p.rows())
	}

	if _, err := p.HandleKey(press(input.KeyUp, 0)); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if p.at != 0 {
		t.Errorf("Up off the top went to %d, want the first row", p.at)
	}
	for range p.rows() + 3 {
		if _, err := p.HandleKey(press(input.KeyDown, 0)); err != nil {
			t.Fatalf("Down: %v", err)
		}
	}
	if p.at != p.rows()-1 {
		t.Errorf("Down off the bottom went to %d, want the last row %d", p.at, p.rows()-1)
	}
}

// A key nobody could read whole is not drawn at all. Half a
// fingerprint is worse than none: it can be compared against another
// and believed to match.
func TestTheSecretsPaneDropsANoteItCannotDrawWhole(t *testing.T) {
	_, _, p := aWindowWithASecretsPane(t)

	// Wide enough for the name and not for the note after it.
	said := strings.Join(secretsPaneText(t, p, 14, 24), "\n")
	if !strings.Contains(said, "margit") {
		t.Fatalf("the name went as well as the note:\n%s", said)
	}
	for _, part := range []string{"kettle", "kett", "ettle", "ke"} {
		if strings.Contains(said, part) {
			t.Errorf("the pane drew part of the note %q:\n%s", part, said)
		}
	}
}

// The pane has a row on the sidebar, and closing it takes the row.
func TestTheSecretsPaneHasARowAndGivesItBack(t *testing.T) {
	a, _, p := aWindowWithASecretsPane(t)
	if a.secretsRow == nil || a.secretsRow.Kind != conns.Secrets {
		t.Fatalf("the pane has no row of its own: %+v", a.secretsRow)
	}
	row := a.secretsRow

	if err := a.closePane(p); err != nil {
		t.Fatalf("close: %v", err)
	}
	if a.secretsPane != nil || a.secretsRow != nil {
		t.Error("the window still holds the pane it closed")
	}
	for _, group := range a.registry.Groups(panelNow) {
		for _, held := range group.Rows {
			if held.Entry == row {
				t.Error("the row is still on the sidebar")
			}
		}
	}
}

// Asking for it twice goes to the one that is open, rather than
// opening a second on the same vault.
func TestTheSecretsPaneOpensOnce(t *testing.T) {
	a, _, p := aWindowWithASecretsPane(t)
	if err := a.showSecretsPane(); err != nil {
		t.Fatalf("open it again: %v", err)
	}
	if a.secretsPane != p {
		t.Error("a second pane opened on the same vault")
	}
}

// The buttons follow the row the bar is on: a key has none of a
// secret's, because what they mean is a secret's.
func TestTheSecretsPaneOffersWhatTheRowCanDo(t *testing.T) {
	_, _, p := aWindowWithASecretsPane(t)
	secretsPaneText(t, p, 80, 24)

	p.at = 0
	if got := p.choices(); !slices.Contains(got, btnChange) || !slices.Contains(got, btnRemove) {
		t.Errorf("a secret offers %v, want a way to change and remove it", got)
	}
	// Onto the first key.
	p.at = len(p.items)
	if got := p.choices(); slices.Contains(got, btnChange) {
		t.Errorf("a key offers %v, which is what a secret offers", got)
	}
}

// Space ticks a row, and Remove then speaks of all of them.
func TestTheSecretsPaneRemovesSeveralAtOnce(t *testing.T) {
	a, v, p := aWindowWithASecretsPane(t)
	secretsPaneText(t, p, 80, 24)
	if len(p.items) != 2 {
		t.Fatalf("%d secrets, want the two that were put in", len(p.items))
	}

	p.at = 0
	if _, err := p.HandleKey(press(input.KeySpace, 0)); err != nil {
		t.Fatalf("space: %v", err)
	}
	p.at = 1
	if _, err := p.HandleKey(press(input.KeySpace, 0)); err != nil {
		t.Fatalf("space: %v", err)
	}
	if len(p.picked) != 2 {
		t.Fatalf("%d rows are ticked, want two", len(p.picked))
	}
	if got := p.removeTitle(); !strings.Contains(got, "2") {
		t.Errorf("the button says %q without saying how many", got)
	}

	if err := p.press(slices.Index(p.choices(), p.removeTitle())); err != nil {
		t.Fatalf("press Remove: %v", err)
	}
	f := awaitModal(t, a, "the question about removing them",
		byTitlePrefix[*ui.Form](dlgRemove))
	pressButton(t, a, f, btnRemove)

	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("%d secrets are left, want both gone", len(items))
	}
}

// Nothing ticked means the row the bar is on, which is the ordinary
// way to remove one.
func TestTheSecretsPaneRemovesTheRowWhenNothingIsTicked(t *testing.T) {
	a, v, p := aWindowWithASecretsPane(t)
	secretsPaneText(t, p, 80, 24)
	p.at = 0
	want := p.items[0].Name

	if err := p.press(slices.Index(p.choices(), btnRemove)); err != nil {
		t.Fatalf("press Remove: %v", err)
	}
	f := awaitModal(t, a, "the question about removing it",
		byTitle[*ui.Form](dlgRemove+want+"?"))
	pressButton(t, a, f, btnRemove)

	items, err := v.Items()
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("%d secrets are left, want the other one", len(items))
	}
	if items[0].Name == want {
		t.Errorf("%q is still there", want)
	}
}

// A row acted on after something else removed it says so, rather than
// handing on the vault's own answer, which reads as a fault here.
func TestTheSecretsPaneSaysWhenARowHasGoneElsewhere(t *testing.T) {
	a, v, p := aWindowWithASecretsPane(t)
	secretsPaneText(t, p, 80, 24)
	p.at = 0
	it := p.items[0]

	// Another window takes it away.
	if err := v.Remove(it.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if err := p.change(v); err != nil {
		t.Fatalf("change: %v", err)
	}
	if up := a.root.Modal(); up != nil {
		t.Errorf("a %T dialog opened on a secret that has gone", up)
	}
	if said := a.saying(); !strings.Contains(said, "removed from somewhere else") {
		t.Errorf("the line says %q, want that it went elsewhere", said)
	}
}

// Changing one from the pane opens the form the chooser opens, and the
// pane is still there behind it.
func TestTheSecretsPaneChangesThroughTheSameForm(t *testing.T) {
	a, v, p := aWindowWithASecretsPane(t)
	secretsPaneText(t, p, 80, 24)
	p.at = 0
	name := p.items[0].Name

	if err := p.press(slices.Index(p.choices(), btnChange)); err != nil {
		t.Fatalf("press Change: %v", err)
	}
	f := awaitModal(t, a, "the change form", byTitle[*ui.Form](changeSecretTitle+" — "+name))
	if f.Field(fldName) == nil || f.Field(fldFor) == nil {
		t.Error("the form is not the one the chooser opens")
	}
	_ = v
}
