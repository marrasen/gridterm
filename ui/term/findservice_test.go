package term

import "testing"

// serviceIn is what findLink says about a column of a line.
func serviceIn(line string, at int) (string, bool) {
	link, _, _, ok := findLink([]rune(line), at)
	return link, ok
}

// A development server prints where it is listening, and that is a
// link whether or not it wrote a scheme in front of it.
func TestWhereAServerIsListeningIsALink(t *testing.T) {
	for _, tc := range []struct {
		line string
		at   int
		want string
	}{
		{"  ➜  Local:   http://localhost:5173/", 25, "http://localhost:5173/"},
		{"listening on localhost:5173", 16, "http://localhost:5173"},
		{"Server started on 127.0.0.1:8080", 22, "http://127.0.0.1:8080"},
		{"bound to 0.0.0.0:3000", 12, "http://0.0.0.0:3000"},
		{"ready at localhost:4321/admin", 12, "http://localhost:4321/admin"},
	} {
		got, ok := serviceIn(tc.line, tc.at)
		if !ok {
			t.Errorf("%q: nothing found at column %d", tc.line, tc.at)
			continue
		}
		if got != tc.want {
			t.Errorf("%q gave %q, want %q", tc.line, got, tc.want)
		}
	}
}

// And what is not one is not one. This is a guess, so it is a narrow
// one: a word ending in the host's name, a host with no port, and a
// number too long to be a port are all just text.
func TestWhatIsNotAListeningAddressIsNotALink(t *testing.T) {
	for _, tc := range []struct {
		line string
		at   int
	}{
		{"mylocalhost:80 is not ours", 4},
		{"reached localhost with no port", 10},
		{"localhost:1234567 is too many digits", 3},
		{"run it on localhost first", 12},
		{"127.0.0.1 alone", 3},
	} {
		if got, ok := serviceIn(tc.line, tc.at); ok {
			t.Errorf("%q gave %q at column %d", tc.line, got, tc.at)
		}
	}
}

// A column outside the address is not in it, so the underline covers
// the address and not the sentence around it.
func TestOnlyTheAddressItselfIsTheLink(t *testing.T) {
	line := []rune("listening on localhost:5173 now")
	_, from, to, ok := findLink(line, 16)
	if !ok {
		t.Fatal("nothing found")
	}
	if got := string(line[from:to]); got != "localhost:5173" {
		t.Errorf("the link covers %q", got)
	}
	if _, in := serviceIn(string(line), 2); in {
		t.Error("a column in the word before it is in the link")
	}
	if _, in := serviceIn(string(line), 29); in {
		t.Error("a column in the word after it is in the link")
	}
}
