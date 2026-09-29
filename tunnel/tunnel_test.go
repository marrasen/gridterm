package tunnel

import (
	"errors"
	"strings"
	"testing"

	"github.com/marrasen/kakel/remote"
)

// Traffic is written with its direction and size, as text, with what
// would draw over the pane as dots and a large chunk cut short.
func TestTrafficIsWrittenAsText(t *testing.T) {
	if got := string(chunkLines(true, []byte("GET /\r\n\x1b[2J"))); got != "→ 11 bytes\nGET /\n.[2J\n" {
		t.Fatalf("a chunk out reads %q", got)
	}
	big := string(chunkLines(false, make([]byte, mostTapBytes+1)))
	if !strings.HasPrefix(big, "← 4097 bytes, first 4096 shown\n") || len(big) != len("← 4097 bytes, first 4096 shown\n")+mostTapBytes+1 {
		t.Fatalf("a large chunk in starts %q and is %d long", big[:40], len(big))
	}
}

func TestASavedTunnelReadsBack(t *testing.T) {
	for _, k := range []remote.TunnelKind{remote.LocalForward, remote.RemoteForward, remote.DynamicForward} {
		want := remote.Tunnel{Kind: k, Listen: "127.0.0.1:8080", Target: "db:5432"}
		saved := Saved("srv", "a1", want)
		if saved.Host != "srv" || saved.HostID != "a1" {
			t.Fatalf("kept as %+v", saved)
		}
		if got, err := Read(saved); err != nil || got != want {
			t.Fatalf("%v reads back as %+v, %v", k, got, err)
		}
	}
	saved := Saved("srv", "", remote.Tunnel{Kind: remote.LocalForward})
	saved.Kind = "sideways"
	if _, err := Read(saved); err == nil {
		t.Fatal("a kind it does not know read back")
	}
}

func TestTheQuestionNamesWhatOpens(t *testing.T) {
	if got := ListenName(remote.Tunnel{Listen: "0.0.0.0:8080"}); got != "0.0.0.0:8080 to the network" {
		t.Fatalf("a port asked for is %q", got)
	}
	if got := ListenName(remote.Tunnel{Listen: "0.0.0.0:0"}); got != "a port on 0.0.0.0 to the network" {
		t.Fatalf("any free port is %q", got)
	}
}

// Only the first failed stream is one to tell about; each is counted
// and written down.
func TestTheFirstFailureIsTheOneTold(t *testing.T) {
	h := New()
	if !h.Failed(errors.New("refused")) || h.Failed(errors.New("refused again")) {
		t.Fatal("the first failure and only it is to be told")
	}
	if h.failed != 2 || h.seen.Held() != 2 {
		t.Fatalf("counted %d, with %d lines written", h.failed, h.seen.Held())
	}
}
