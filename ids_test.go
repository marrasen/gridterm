package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/marrasen/kakel/remote"
)

// idsAsNames gives each server saved in b its name for its ID, as the
// tests name the machines they connect to: a.machines.Get("srv").Conn is then the
// connection to the saved server srv. Tests of a name and an ID that
// differ, as after a rename, keep the IDs the list gave.
func idsAsNames(t *testing.T, b *remote.Book) {
	t.Helper()
	raw, err := os.ReadFile(b.Path())
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Version int              `json:"version"`
		Servers []map[string]any `json:"servers"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	named := map[string]string{}
	for _, s := range f.Servers {
		named[s["id"].(string)] = s["name"].(string)
	}
	for _, s := range f.Servers {
		s["id"] = s["name"]
		if via, ok := s["via"].(string); ok && via != "" {
			s["via"] = named[via]
		}
	}
	out, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.Path(), out, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.Reload(); err != nil {
		t.Fatal(err)
	}
}

// quickCount is how many quick connections a knows.
func quickCount(a *app) int {
	n := 0
	for _, m := range a.machines.Infos() {
		if m.Quick {
			n++
		}
	}
	return n
}
