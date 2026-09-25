package input

import "testing"

func TestPasteIsBracketedWhenTheProgramAsked(t *testing.T) {
	got := string(EncodePaste("hi", true, nil))
	if got != "\x1b[200~hi\x1b[201~" {
		t.Fatalf("= %q, want it wrapped in paste markers", got)
	}
}

func TestPasteIsBareWhenTheProgramDidNot(t *testing.T) {
	got := string(EncodePaste("hi", false, nil))
	if got != "hi" {
		t.Fatalf("= %q, want no markers", got)
	}
}

func TestPasteNewlinesBecomeCarriageReturns(t *testing.T) {
	got := string(EncodePaste("a\nb", false, nil))
	if got != "a\rb" {
		t.Fatalf("= %q, want a carriage return, which is what Enter sends", got)
	}
}

// A CRLF pair must not become two returns, or every pasted line runs
// twice.
func TestPasteCRLFBecomesOneReturn(t *testing.T) {
	got := string(EncodePaste("a\r\nb", false, nil))
	if got != "a\rb" {
		t.Fatalf("= %q, want one carriage return", got)
	}
}

// Pasted text is data. An escape sequence hidden in it would otherwise
// be obeyed, which is how a copied web page runs a command.
func TestPasteDropsControlCharacters(t *testing.T) {
	got := string(EncodePaste("a\x1b[31mb\x07c", false, nil))
	if got != "a[31mbc" {
		t.Fatalf("= %q, want the escape and the bell dropped", got)
	}
}

func TestPasteKeepsTabs(t *testing.T) {
	got := string(EncodePaste("a\tb", false, nil))
	if got != "a\tb" {
		t.Fatalf("= %q, want the tab kept", got)
	}
}

func TestPasteKeepsNonASCII(t *testing.T) {
	got := string(EncodePaste("café 世界", false, nil))
	if got != "café 世界" {
		t.Fatalf("= %q, want the text unchanged", got)
	}
}

func TestPasteEmptyIsNothing(t *testing.T) {
	if got := EncodePaste("", true, nil); len(got) != 0 {
		t.Fatalf("= %q, want nothing at all — not even the markers", got)
	}
}

func TestEncodePasteAppendsToCallerBuffer(t *testing.T) {
	buf := []byte("x")
	buf = EncodePaste("y", false, buf)
	if string(buf) != "xy" {
		t.Fatalf("= %q, want \"xy\"", buf)
	}
}

// Two newlines are a blank line, which the paste keeps, in either
// line ending.
func TestPasteKeepsABlankLine(t *testing.T) {
	for in, want := range map[string]string{
		"a\n\nb":         "a\r\rb",
		"a\r\n\r\nb":     "a\r\rb",
		"a\r\rb":         "a\r\rb",
		"a\n\n\nb\r\n\n": "a\r\r\rb\r\r",
	} {
		if got := string(EncodePaste(in, false, nil)); got != want {
			t.Errorf("EncodePaste(%q) = %q, want %q", in, got, want)
		}
	}
}
