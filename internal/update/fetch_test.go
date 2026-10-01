package update

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// zipOf is a zip holding name with body.
func zipOf(t *testing.T, name, body string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(body))
	_ = zw.Close()
	return b.Bytes()
}

// A release's program is fetched, checked against SHA256SUMS, and
// written beside the one running; one that does not match is refused.
func TestAReleaseIsFetchedAndChecked(t *testing.T) {
	archive := zipOf(t, "kakel.exe", "the new kakel")
	sum := sha256.Sum256(archive)
	sums := hex.EncodeToString(sum[:]) + "  kakel_v9.0.0_windows_amd64.zip\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.zip":
			_, _ = w.Write(archive)
		case "/sums":
			_, _ = w.Write([]byte(sums))
		case "/bad":
			_, _ = w.Write([]byte("0000  kakel_v9.0.0_windows_amd64.zip\n"))
		}
	}))
	defer srv.Close()
	exe := filepath.Join(t.TempDir(), "kakel.exe")
	r := Release{Version: "v9.0.0", Assets: map[string]string{"kakel_v9.0.0_windows_amd64.zip": srv.URL + "/a.zip", "SHA256SUMS": srv.URL + "/sums"}}
	part, err := Fetch(t.Context(), r, "windows", "amd64", exe)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(part); string(got) != "the new kakel" || part != exe+".new" {
		t.Fatalf("fetched %q to %s", got, part)
	}
	r.Assets["SHA256SUMS"] = srv.URL + "/bad"
	if _, err := Fetch(t.Context(), r, "windows", "amd64", exe); err == nil {
		t.Fatal("an archive that does not match its sum was taken")
	}
	if _, err := Fetch(t.Context(), r, "linux", "amd64", exe); err == nil {
		t.Fatal("a release with no archive for the system was taken")
	}
}
