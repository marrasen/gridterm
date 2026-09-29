package links

import "testing"

func TestOnlyWebAndMailLinksOpen(t *testing.T) {
	for at, ok := range map[string]bool{
		"https://example.com": true, "mailto:a@example.com": true,
		"file:///etc/passwd": false, "javascript:alert(1)": false, "": false,
	} {
		if err := Openable(at); (err == nil) != ok {
			t.Errorf("%q: %v", at, err)
		}
	}
	if target, ok := LocalService("http://localhost:3000/app"); !ok || target != "127.0.0.1:3000" {
		t.Errorf("localhost:3000 goes to %q, %v", target, ok)
	}
	if _, ok := LocalService("http://example.com:3000/app"); ok {
		t.Error("an address elsewhere was taken for a local service")
	}
}

// A path a shell on a far Windows machine prints from the top of a
// drive is looked for at the top of that drive.
func TestAWindowsPathFromTheTopOfADriveStandsAlone(t *testing.T) {
	for p, abs := range map[string]bool{
		`C:\Users\x`: true, "d:/x": true, `C:\`: true,
		"C:": false, "a:b.jar": false, "notes.txt": false, "/home/x": false, "1:/x": false,
	} {
		if WindowsAbs(p) != abs {
			t.Errorf("%q from the top of a drive: %v, want %v", p, !abs, abs)
		}
	}
}

// A server's words give the one link in them, and none when they give
// several or none; the marks around a link are not part of it.
func TestTheOneLinkInAServersWords(t *testing.T) {
	for _, c := range []struct {
		lines []string
		want  string
	}{
		{[]string{"Sign in at <https://sso.example/device>, then enter ABCD."}, "https://sso.example/device"},
		{[]string{"Go to https://a.example", "or https://a.example again"}, "https://a.example"},
		{[]string{"https://a.example or https://b.example"}, ""},
		{[]string{"nothing to open here, not even https://"}, ""},
		{[]string{"javascript:alert(1)"}, ""},
	} {
		got, ok := Only(c.lines)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%q gives %q, %v; want %q", c.lines, got, ok, c.want)
		}
	}
}
