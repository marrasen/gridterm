package secrets

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

// A password with the separator in it comes back whole.
//
// A comma, a quote and a newline are all legal in a password and all
// break a file written by joining strings. Anything that read this
// would take the next field as part of the password, or the next line
// as another secret.
func TestAwkwardValuesSurviveTheExport(t *testing.T) {
	v, _, _ := aVault(t)
	awkward := map[string]string{
		"comma":   `a,b`,
		"quote":   `say "hello"`,
		"newline": "one\ntwo",
		"both":    `"a,b" and` + "\n" + `more`,
	}
	for name, value := range awkward {
		if _, err := v.Put(Item{Name: name}, value); err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
	}

	out, err := v.Everything()
	if err != nil {
		t.Fatalf("everything: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, out); err != nil {
		t.Fatalf("write: %v", err)
	}

	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	got := map[string]string{}
	for _, row := range rows[1:] {
		got[row[0]] = row[3]
	}
	for name, want := range awkward {
		if got[name] != want {
			t.Errorf("%s came back as %q, want %q", name, got[name], want)
		}
	}
}

// A locked vault hands nothing over, the way every other way in does.
func TestALockedVaultExportsNothing(t *testing.T) {
	v, _, _ := aVault(t)
	if _, err := v.Put(Item{Name: "one"}, "a"); err != nil {
		t.Fatalf("put: %v", err)
	}
	v.Lock()

	if _, err := v.Everything(); err == nil {
		t.Fatal("a locked vault handed over everything in it")
	}
}

// The header is what it says it is, in the order the rows follow.
func TestTheHeaderMatchesTheRows(t *testing.T) {
	v, _, _ := aVault(t)
	if _, err := v.Put(Item{Name: "one", User: "who"}, "value"); err != nil {
		t.Fatalf("put: %v", err)
	}
	out, err := v.Everything()
	if err != nil {
		t.Fatalf("everything: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, out); err != nil {
		t.Fatalf("write: %v", err)
	}

	rows, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("%d lines, want a header and one secret", len(rows))
	}
	if len(rows[0]) != len(rows[1]) {
		t.Errorf("the header has %d columns and the row %d", len(rows[0]), len(rows[1]))
	}
	for i, head := range rows[0] {
		if head != csvHeader[i] {
			t.Errorf("column %d is %q, want %q", i, head, csvHeader[i])
		}
	}
}
