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
