package single

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// A kakel started while one runs hands its command line over, and the
// one running hears it.
func TestACommandLineIsHandedOver(t *testing.T) {
	dir := t.TempDir()
	if ok, err := Hand(dir, Handover{Args: []string{"-ssh", "x"}}); ok || err != nil {
		t.Fatalf("with none running, the handover was taken %v, %v", ok, err)
	}
	got, err := Listen(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := Hand(dir, Handover{Args: []string{"-ssh", "x"}, Dir: "/srv"})
	if !ok || err != nil {
		t.Fatalf("the handover was taken %v, %v", ok, err)
	}
	select {
	case h := <-got:
		if !slices.Equal(h.Args, []string{"-ssh", "x"}) || h.Dir != "/srv" {
			t.Fatalf("the one running heard %+v", h)
		}
	case <-time.After(time.Second):
		t.Fatal("the one running heard nothing")
	}
	if fi, err := os.Stat(filepath.Join(dir, File)); err != nil || fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the file is %v, %v", fi.Mode(), err)
	}
}

// A handover without the token is not heard.
func TestAHandoverWithoutTheTokenIsRefused(t *testing.T) {
	dir := t.TempDir()
	got, err := Listen(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, File))
	var r running
	_ = json.Unmarshal(raw, &r)
	conn, err := net.Dial("tcp", r.Addr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte(`{"token":"wrong","args":["-e","rm"]}` + "\n"))
	buf := make([]byte, 8)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	n, _ := conn.Read(buf)
	_ = conn.Close()
	if n != 0 {
		t.Fatalf("a wrong token was answered %q", buf[:n])
	}
	select {
	case h := <-got:
		t.Fatalf("a wrong token was heard: %+v", h)
	case <-time.After(100 * time.Millisecond):
	}
}

// The file goes once the one running stops, and a kakel starting then
// runs as the one.
func TestTheFileGoesWithTheOneRunning(t *testing.T) {
	dir := t.TempDir()
	ctx, stop := context.WithCancel(t.Context())
	if _, err := Listen(ctx, dir); err != nil {
		t.Fatal(err)
	}
	stop()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, File)); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the file stayed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ok, _ := Hand(dir, Handover{}); ok {
		t.Fatal("a handover was taken with none running")
	}
}
