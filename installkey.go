package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// installKeyCommand and installKeyTitle are the command that adds a
// public key to a server's authorized_keys, and the dialog it opens.
const (
	installKeyCommand = "sshkey.install"
	installKeyTitle   = "Install SSH Key"
)

// installKeyRow is the command's row under the SSH Keys header of the
// Servers menu, which already says the SSH.
const installKeyRow = "Install Key…"

// couldNotInstallKey heads whatever went wrong on the way to the key
// being on the server.
const couldNotInstallKey = "Could not install the key"

// keyPartlyInstalled heads a key that went into one file a server reads
// and could not go into another.
const keyPartlyInstalled = "Key installed, but not in every file"

// installKeyHere asks which key to put on which server, starting from
// the machine whose row was clicked or the one the focused pane is on.
func (a *app) installKeyHere() error {
	h, err := a.here()
	if err != nil {
		return err
	}
	servers := a.keyServers(h)
	if len(servers) == 0 {
		return errors.New("No saved servers")
	}
	at := servers[0].Key
	if slices.ContainsFunc(servers, func(c ui.Choice) bool { return c.Key == h.name }) {
		at = h.name
	}
	a.openInstallKey(servers, at)
	return nil
}

// keyServers are the machines a key can be installed on: every saved
// one that is logged in to, and the one the user is looking at when it
// is connected without being saved.
func (a *app) keyServers(h hostFacts) []ui.Choice {
	var out []ui.Choice
	if (h.kind == hostMachine || h.kind == hostConnecting) && !h.saved {
		out = append(out, ui.Choice{Key: h.name, Label: groupName(h.name)})
	}
	for _, s := range a.book.Hosts() {
		if !s.Window {
			out = append(out, ui.Choice{Key: s.Name, Label: s.Name})
		}
	}
	return out
}

// openInstallKey asks which key to add to which server.
func (a *app) openInstallKey(servers []ui.Choice, at string) {
	f := a.newForm(installKeyTitle)
	server := f.AddField(fldServer, a.newField("", 0))
	server.Choices = servers
	server.SetText(at)
	key := f.AddField(fldKeyFile, a.newField("Private key path", 0))
	key.Options = a.keyFiles.all()
	key.Hint = "The .pub file beside it is what the server gets"
	// The server's own key when it has one: that is the one it is going
	// to be asked to accept. The newest one kept otherwise. Changing the
	// server changes it only while it is still the one picked here: a
	// path the user typed stays.
	picked := ""
	pick := func() {
		if key.Text() != picked {
			return
		}
		if own := firstIdentity(a.about(server.Text()).record()); own != "" {
			picked = own
		} else if len(key.Options) > 0 {
			picked = key.Options[0]
		}
		key.SetText(picked)
	}
	pick()
	server.OnChange = func(string) {
		pick()
		a.markDirty()
	}
	a.completePath(key, vfs.NewLocal())

	f.AddButton(ui.Button{Title: btnAdd, Do: func() error {
		typed := strings.TrimSpace(key.Text())
		if typed == "" {
			return errNoKeyFile
		}
		at, err := fromHome(typed)
		if err != nil {
			return err
		}
		line, err := publicKeyLine(at)
		if err != nil {
			// Returned rather than shown here, so the dialog stays open
			// with the path still there to correct.
			return err
		}
		name := server.Text()
		// Not from here: this dialog closes as soon as this returns, and
		// connecting may put up a dialog of its own to ask for a
		// password.
		a.pump.post(func() { a.installKeyOn(name, line) })
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	a.showForm(f, nil)
}

// publicKeyLine reads the public half of a key file, as the one line an
// authorized_keys file takes.
//
// The path may name either half. A private key's public half is the
// .pub beside it, the way ssh-keygen writes them and New SSH Key does.
func publicKeyLine(path string) (string, error) {
	pub := path
	if !strings.HasSuffix(pub, ".pub") {
		pub += ".pub"
	}
	b, err := os.ReadFile(pub)
	if errors.Is(err, fs.ErrNotExist) {
		return "", errNoPublicKey
	}
	if err != nil {
		return "", err
	}
	// Written again from the key rather than copied, so a comment line
	// above it or anything after it stays behind.
	key, comment, _, _, err := ssh.ParseAuthorizedKey(b)
	if err != nil {
		return "", errNotAPublicKey
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	if comment != "" {
		line += " " + comment
	}
	return line, nil
}

// What the dialog says about the key file it was given.
var (
	errNoKeyFile     = errors.New("Enter a key file")
	errNoPublicKey   = errors.New("Public key not found")
	errNotAPublicKey = errors.New("Invalid public key")
)

// installKeyOn connects to a server, when nothing is connected to it
// yet, and adds a public key to the authorized_keys there.
func (a *app) installKeyOn(name, line string) {
	on := step{name: name}
	if h := a.about(name); h.saved {
		on.id = h.record().ID
	}
	a.filesystemAgain(name, on, a.savedCalledNow(on.id), func(f vfs.FS, _ step, err error) {
		if err != nil {
			a.reportError(couldNotInstallKey, err)
			return
		}
		// Off the goroutine that draws: every step is a round trip to
		// the server.
		go func() {
			unlock := lockInstalls(f.Place())
			got, err := installKey(f, line)
			unlock()
			// Not part of what is reported: the key is written or not
			// by the time the session closes.
			closed := f.Close()
			a.pump.post(func() {
				if closed != nil {
					a.logError(closed)
				}
				switch {
				case err != nil && got.has:
					a.reportError(keyPartlyInstalled, err)
				case err != nil:
					a.reportError(couldNotInstallKey, err)
				case got.added:
					a.say(keyInstalledOn(name))
				default:
					a.say(keyAlreadyOn(name))
				}
			})
		}()
	})
}

// keyInstalledOn and keyAlreadyOn are what the status line says when
// the key is on the server.
func keyInstalledOn(name string) string { return "Key installed on " + groupName(name) }
func keyAlreadyOn(name string) string   { return groupName(name) + " already has this key" }

// installing is a lock for each machine a key is being installed on,
// by the place its filesystem reads.
//
// Two installs into one file at once would both read it, both find
// their key missing, and both write at the end they read. OpenSSH
// appends wherever it is told to write, but a server that writes where
// it is told -- the one a gridterm window serves -- would put the
// second key over the first, and both would report it installed.
//
// One per machine rather than one for the window: a server that stops
// answering half way holds up installs on itself and no others. A lock
// is left behind for each connection a key was installed over, which
// is a handful in the life of a window.
var installing sync.Map

// lockInstalls waits for any install on a machine to finish, and hands
// back what lets the next one start.
func lockInstalls(place any) func() {
	held, _ := installing.LoadOrStore(place, new(sync.Mutex))
	mu := held.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// installed is where a key stands after an install: whether it was
// added anywhere, and whether any file the server reads has it now,
// added or already there.
type installed struct{ added, has bool }

// windowsHome is how Windows' OpenSSH names a home directory over SFTP:
// a drive letter behind a slash, /C:/Users/me.
var windowsHome = regexp.MustCompile(`^/[A-Za-z]:(/|$)`)

// installKey adds a public key line to the authorized_keys a server
// reads for the user logged in, and reports whether it had to: a server
// that has the key already is left as it was.
//
// On Windows that is two files. OpenSSH there reads
// administrators_authorized_keys for an administrator and ignores the
// one in their home. Only an administrator can open it, so trying is
// what tells the two kinds of account apart, and a refusal means the
// home one is the file that counts. The home one is written either way:
// a server whose sshd_config drops the administrators rule reads that
// one for everybody. So it is written even when the shared one could
// not be, and the two failures are reported together.
func installKey(f vfs.FS, line string) (installed, error) {
	add, ok := f.(vfs.Appender)
	if !ok {
		return installed{}, fmt.Errorf("%s cannot add to a file in place", f.Name())
	}
	want, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return installed{}, err
	}
	home, err := f.Home()
	if err != nil {
		return installed{}, err
	}
	home = strings.TrimSuffix(home, "/")

	var got installed
	var shared error
	if windowsHome.MatchString(home) {
		got, shared = addAdministratorKey(f, add, home, want, line)
	}
	did, err := addHomeKey(f, add, home, want, line)
	if err == nil {
		got.added = got.added || did
		got.has = true
	}
	return got, errors.Join(shared, err)
}

// addHomeKey adds a key to the authorized_keys in a home directory,
// making the .ssh it goes in when there is none.
func addHomeKey(f vfs.FS, add vfs.Appender, home string, want ssh.PublicKey, line string) (bool, error) {
	dir := home + "/.ssh"
	switch e, err := f.Stat(dir); {
	case errors.Is(err, fs.ErrNotExist):
		// One made meanwhile by another install is as good as this one.
		if err := f.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return false, err
		}
	case err != nil:
		return false, err
	case !e.IsDir() && !e.IsLink():
		// A link is let through: a .ssh kept somewhere else and linked
		// from home is common, and the writes below follow it.
		return false, fmt.Errorf("%s is a file", dir)
	}
	return addKeyTo(f, add, dir+"/authorized_keys", want, line)
}

// addAdministratorKey adds a key to the administrators_authorized_keys
// of a Windows server, when the account logged in is an administrator.
//
// ProgramData is on the system drive, which is almost always C: and
// need not be the drive home is on.
func addAdministratorKey(f vfs.FS, add vfs.Appender, home string, want ssh.PublicKey, line string) (installed, error) {
	for _, drive := range slices.Compact([]string{home[:3], "/C:"}) {
		shared := drive + "/ProgramData/ssh"
		if e, err := f.Stat(shared); err != nil || !e.IsDir() && !e.IsLink() {
			continue
		}
		did, err := addKeyTo(f, add, shared+"/administrators_authorized_keys", want, line)
		var refused *refusedError
		switch {
		case errors.As(err, &refused):
			// Not an administrator, so not a file sshd reads for this
			// account.
			return installed{}, nil
		case err != nil:
			return installed{}, err
		}
		return installed{added: did, has: true}, nil
	}
	return installed{}, nil
}

// refusedError is a file this account may not read or make, as against
// one it read and then could not write. Only an administrator can read
// administrators_authorized_keys, so failing to write one that was read
// is a failure, not a sign of who is logged in.
type refusedError struct{ err error }

func (r *refusedError) Error() string { return r.err.Error() }
func (r *refusedError) Unwrap() error { return r.err }

// refused marks a failure to open a file as the account not being
// allowed to, when that is what it was.
func refused(err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return &refusedError{err}
	}
	return err
}

// addKeyTo adds a key to the end of one authorized_keys file, unless
// the key is in it already.
//
// Added to in place, never written whole. The keys already there are
// what the user may be logged in with, and a write that fails half way
// leaves them as they were and a broken last line sshd skips. A file
// added to also keeps its owner and permissions, which sshd checks, and
// on Windows the ACL it was given by hand.
//
// A file that is not there is made readable by its owner alone. On
// Windows that mode means nothing, and the file takes its ACL from the
// directory it is made in.
func addKeyTo(f vfs.FS, add vfs.Appender, path string, want ssh.PublicKey, line string) (bool, error) {
	for again := false; ; again = true {
		r, err := f.Open(path)
		if errors.Is(err, fs.ErrNotExist) {
			w, err := add.CreateNew(path, 0o600)
			if err == nil {
				return writeLine(w, line)
			}
			// Made meanwhile, by another install into the same file.
			// Created over, it would lose the key that one wrote, so
			// this adds to it instead.
			if _, there := f.Stat(path); there == nil && !again {
				continue
			}
			return false, refused(err)
		}
		if err != nil {
			return false, refused(err)
		}
		was, err := io.ReadAll(r)
		if err = errors.Join(err, r.Close()); err != nil {
			return false, err
		}
		if hasKey(was, want) {
			return false, nil
		}
		w, err := add.Append(path)
		if err != nil {
			return false, err
		}
		if len(was) > 0 && was[len(was)-1] != '\n' {
			line = "\n" + line
		}
		return writeLine(w, line)
	}
}

// writeLine writes one line to a file and closes it.
func writeLine(w io.WriteCloser, line string) (bool, error) {
	_, err := io.WriteString(w, line+"\n")
	if err = errors.Join(err, w.Close()); err != nil {
		return false, err
	}
	return true, nil
}

// hasKey reports whether an authorized_keys file lets a key in already.
// The comment and any options are not compared: it is the key itself
// that the server matches, and a key already there with options on it
// is one somebody restricted on purpose.
func hasKey(file []byte, want ssh.PublicKey) bool {
	marshalled := want.Marshal()
	lines := bufio.NewScanner(bytes.NewReader(file))
	lines.Buffer(nil, 1<<20)
	for lines.Scan() {
		got, _, _, _, err := ssh.ParseAuthorizedKey(lines.Bytes())
		if err == nil && bytes.Equal(got.Marshal(), marshalled) {
			return true
		}
	}
	return false
}
