package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/clip"
	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/secrets"
)

// The secrets, on the program's side: gridterm's vault, in the same
// file gridterm keeps it in. The vault opens with a key the window has
// already unlocked to reach a server, or asks to unlock one, or asks
// for the vault's own passphrase when no key of its is here.
//
// The window is sent the names and details of what is in it, and a
// secret itself only when the user copies, types or shows it.

// Secrets is the vault as the window shows it.
type Secrets struct {
	// Exists is set once there is a vault, and Open while it is
	// unlocked.
	Exists, Open bool
	Items        []SecretItem
	Keys         []SecretKey
	// Passphrase says a passphrase opens the vault as well as its keys.
	Passphrase bool
	// Waiting names the terminal used last when an agent has asked for a
	// secret there, which Type answers, return and all.
	Waiting string
}

// SecretItem is one secret, without the secret.
type SecretItem struct {
	ID, Name, User, File string
	Kind                 secrets.Kind
}

// SecretKey is something that opens the vault: a key, or the vault's
// passphrase.
type SecretKey struct {
	Name, Note, Fingerprint string
	Passphrase              bool
	// Removing says what is left once it goes.
	Removing string
}

// Intents for the secrets.
type (
	// ShowSecrets opens the secrets pane, unlocking the vault, or
	// offering to start one.
	ShowSecrets struct{}
	// UnlockSecrets unlocks the vault, asking whatever it takes.
	UnlockSecrets struct{}
	// LockSecrets locks the vault.
	LockSecrets struct{}
	// PutSecret keeps a secret: a new one when ID is empty. An empty
	// Value on a change keeps the value saved.
	PutSecret struct {
		ID, Name, User string
		Kind           secrets.Kind
		Value          string
	}
	// RemoveSecret takes a secret out of the vault, and RemoveSecrets
	// several at once.
	RemoveSecret  struct{ ID string }
	RemoveSecrets struct{ IDs []string }
	// CopySecret puts a secret on the clipboard, for half a minute.
	CopySecret struct{ ID string }
	// TypeSecret types a secret into the terminal used last.
	TypeSecret struct{ ID string }
	// RevealSecret shows a secret in a dialog.
	RevealSecret struct{ ID string }
)

// kindSecrets is the secrets pane.
const kindSecrets = "secrets"

// clipboardHolds is how long a copied secret stays on the clipboard.
const clipboardHolds = 30

// vault is the vault, read off disk the first time anything asks. It
// is locked until a key that fits is offered.
func (a *app) vault() (*secrets.Vault, error) {
	if a.secrets != nil {
		return a.secrets, nil
	}
	path := a.secretsAt
	if path == "" {
		dir, err := conf.Dir()
		if err != nil {
			return nil, fmt.Errorf("find somewhere for the secrets: %w", err)
		}
		path = filepath.Join(dir, secrets.Name)
	}
	v, err := secrets.Open(path)
	if err != nil {
		return nil, err
	}
	a.secrets = v
	return v, nil
}

// withSecrets runs then on the open vault: at once when a key in hand
// opens it, or once the user has unlocked it. With no vault yet, it
// offers to start one. Failures are told, as then may run later.
func (a *app) withSecrets(what string, then func(*secrets.Vault) error) {
	v, err := a.vault()
	if err != nil {
		a.notify(what, err.Error(), "")
		return
	}
	run := func() {
		if err := then(v); err != nil {
			a.notify(what, err.Error(), "")
		}
		a.showVault()
	}
	switch {
	case !v.Exists():
		go a.offerAVault(run)
	case !v.Locked() || v.Unlock(a.ring.Signers()) == nil:
		run()
	default:
		go a.unlockVault(v, what, run)
	}
}

// offerAVault asks whether to start the secrets, on which key when
// there are several, and starts them. It runs on a goroutine of its
// own, as asking waits.
func (a *app) offerAVault(then func()) {
	keys := a.knownKeys()
	if len(keys) == 0 {
		a.events <- func() {
			a.notify("No key to lock the secrets with", "An ed25519 key is needed. Make one with ssh-keygen -t ed25519.", "")
		}
		return
	}
	q := Ask{Title: "No secrets yet", Text: keys[0] + " will open them.", Yes: "Create"}
	if len(keys) > 1 {
		q = Ask{Title: "No secrets yet", Text: "Choose the key that opens them. It is the only one until you add another."}
		for _, k := range keys {
			q.Choose = append(q.Choose, filepath.Base(k))
		}
	}
	ans, err := a.ask(a.ctx, q)
	if err != nil || !ans.Yes {
		return
	}
	keyFile := keys[0]
	if len(keys) > 1 {
		i := slices.Index(q.Choose, ans.Answers[len(ans.Answers)-1])
		if i < 0 {
			return
		}
		keyFile = keys[i]
	}
	if warn := warningsAboutKey(a.ring, nil, keyFile); warn != "" {
		ans, err := a.ask(a.ctx, Ask{Title: "Keep the secrets on " + keyFile + "?", Text: warn, Yes: "Create", Danger: true})
		if err != nil || !ans.Yes {
			return
		}
	}
	signer, err := a.ring.Unlock(a.ctx, keyFile, newAsker(a, ""))
	a.events <- func() {
		if err == nil {
			err = a.makeVault(signer, keyFile)
		}
		if err != nil {
			if !errors.Is(err, errDeclined) && !errors.Is(err, context.Canceled) {
				a.notify("Couldn't create the secrets", err.Error(), "")
			}
			return
		}
		a.notify("Secrets created", keyFile+" opens them.", "")
		then()
	}
}

// makeVault writes a new vault, opened by signer.
func (a *app) makeVault(signer ssh.Signer, keyFile string) error {
	old, err := a.vault()
	if err != nil {
		return err
	}
	v, err := secrets.Create(old.Path(), signer, keyFile)
	if err != nil {
		return err
	}
	a.secrets = v
	return nil
}

// unlockVault unlocks the vault with the key it remembers here, asking
// for that key's passphrase, and asks for the vault's own passphrase
// when no key of its is here or the one here is refused. It runs on a
// goroutine of its own.
func (a *app) unlockVault(v *secrets.Vault, what string, then func()) {
	done := func(err error) {
		a.events <- func() {
			switch {
			case err == nil:
				then()
			case errors.Is(err, errDeclined), errors.Is(err, context.Canceled):
			default:
				a.notify(what, err.Error(), "")
			}
		}
	}
	keyFile, err := keyFileForVault(v)
	if err == nil {
		var signer ssh.Signer
		signer, err = a.ring.Unlock(a.ctx, keyFile, newAsker(a, ""))
		if err == nil {
			err = v.Unlock([]ssh.Signer{signer})
		}
		if err == nil || errors.Is(err, errDeclined) || !v.TakesAPassphrase() {
			done(err)
			return
		}
	} else if !v.TakesAPassphrase() {
		done(err)
		return
	}
	for wrong := 0; ; wrong++ {
		q := Ask{Title: "Unlock Secrets", Prompts: []string{"Passphrase"}, Secret: []bool{true}, Yes: "Unlock"}
		if wrong > 0 {
			q.Text = "That passphrase did not open the secrets. Try again."
		}
		ans, err := a.ask(a.ctx, q)
		if err != nil || !ans.Yes {
			done(errDeclined)
			return
		}
		if err := v.UnlockWith(ans.Answers[0]); !errors.Is(err, secrets.ErrWrongPassphrase) {
			done(err)
			return
		}
	}
}

// keyFileForVault is the key file to unlock to open the vault: one of
// its keys on this machine, or else the first it remembers, whose
// error names the file to bring over.
func keyFileForVault(v *secrets.Vault) (string, error) {
	first := ""
	for _, s := range v.Keys() {
		if s.KeyFile == "" {
			continue
		}
		if onThisMachine(s) {
			return s.KeyFile, nil
		}
		if first == "" {
			first = s.KeyFile
		}
	}
	if first != "" {
		return first, nil
	}
	return "", errors.New("the secrets name no key file that opens them; connect to a server with their key first")
}

// onThisMachine reports whether a slot's key file is here.
func onThisMachine(s secrets.KeySlot) bool {
	if s.KeyFile == "" {
		return false
	}
	_, err := os.Stat(s.KeyFile)
	return err == nil
}

// vaultKeys are the key files a vault could be kept on: the usual one
// and those in ~/.ssh, when their public half is ed25519, the one kind
// that signs the same way every time.
func vaultKeys() []string { return vaultKeysWith(nil) }

// knownKeys are the key files a vault could be kept on, the ones this
// window made or was told of first.
func (a *app) knownKeys() []string {
	if a.settings == nil {
		return vaultKeys()
	}
	return vaultKeysWith(a.settings.Keys)
}

// vaultKeysWith is vaultKeys, with the key files kept, as gridterm
// keeps them, first.
func vaultKeysWith(kept func() []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		if _, err := os.Stat(path); err != nil {
			return
		}
		pub, err := os.ReadFile(path + ".pub")
		if err != nil {
			return
		}
		if key, _, _, _, err := ssh.ParseAuthorizedKey(pub); err != nil || key.Type() != ssh.KeyAlgoED25519 {
			return
		}
		out = append(out, path)
	}
	if kept != nil {
		for _, k := range kept() {
			add(k)
		}
	}
	mine, err := remote.DefaultKeyPath()
	if err == nil {
		add(mine)
		pubs, _ := filepath.Glob(filepath.Join(filepath.Dir(mine), "*.pub"))
		for _, p := range pubs {
			add(p[:len(p)-len(".pub")])
		}
	}
	return out
}

// warningsAboutKey says what the user should know before trusting a
// key with the secrets, or "".
func warningsAboutKey(ring *remote.Ring, v *secrets.Vault, keyFile string) string {
	var out []string
	if agentHoldsKey(ring, keyFile) {
		out = append(out, "A server you forward the agent to can open any copy of the secrets it has.")
	}
	if v != nil {
		if _, err := v.PassphraseFor(keyFile); err == nil {
			out = append(out, "Its passphrase is in the secrets, so another key is still needed to open them.")
		}
	}
	return joinLines(out)
}

// agentHoldsKey reports whether the SSH agent holds a key file's key.
func agentHoldsKey(ring *remote.Ring, keyFile string) bool {
	if ring.AgentTrouble() != nil {
		return false
	}
	pub, err := os.ReadFile(keyFile + ".pub")
	if err != nil {
		return false
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(pub)
	if err != nil {
		return false
	}
	held, err := remote.AgentHolds(secrets.Fingerprint(key))
	return err == nil && held
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n\n"
		}
		out += l
	}
	return out
}

// showVault publishes what the vault holds, the names alone.
// watchVault reads the vault again each second while the secrets pane
// is open, as gridterm's pane does: much of what changes it happens in
// another window, or another program altogether.
func (a *app) watchVault() {
	if a.watchingVault {
		return
	}
	a.watchingVault = true
	go func() {
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-time.After(vaultSettles):
			}
			done := make(chan bool, 1)
			a.events <- func() {
				open := slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.Kind == kindSecrets })
				if !open {
					a.watchingVault = false
					done <- true
					return
				}
				was := a.st.Secrets
				a.showVault()
				if secretsSame(was, a.st.Secrets) {
					a.quiet = true
				}
				done <- false
			}
			if <-done {
				return
			}
		}
	}()
}

// vaultSettles is how often an open secrets pane reads the vault again.
const vaultSettles = time.Second

// secretsSame reports whether two readings of the vault show the same.
func secretsSame(x, y Secrets) bool {
	return x.Exists == y.Exists && x.Open == y.Open && x.Passphrase == y.Passphrase &&
		slices.Equal(x.Items, y.Items) && slices.Equal(x.Keys, y.Keys)
}

func (a *app) showVault() {
	v := a.secrets
	if v == nil {
		a.st.Secrets = Secrets{}
		return
	}
	s := Secrets{Exists: v.Exists(), Open: !v.Locked(), Passphrase: v.TakesAPassphrase()}
	if s.Open {
		items, _ := v.Items()
		for _, it := range items {
			s.Items = append(s.Items, SecretItem{ID: it.ID, Name: it.Name, User: it.User, File: it.File, Kind: it.Kind})
		}
		for _, k := range v.Keys() {
			key := secretKey(k)
			key.Removing = whatRemovingCosts(v, k)
			s.Keys = append(s.Keys, key)
		}
	}
	a.st.Secrets = s
}

// secretKey is a slot as the pane lists it.
func secretKey(k secrets.KeySlot) SecretKey {
	if k.ByPassphrase() {
		return SecretKey{Name: "Passphrase", Note: "a way in without a key", Fingerprint: k.Fingerprint, Passphrase: true}
	}
	name, note := k.KeyFile, k.Fingerprint
	if name == "" {
		name, note = k.Fingerprint, ""
	}
	if onThisMachine(k) {
		note = "on this machine · " + note
	}
	return SecretKey{Name: name, Note: note, Fingerprint: k.Fingerprint}
}

// showSecretsPane opens the secrets pane, or goes to it, once the
// vault is open.
func (a *app) showSecretsPane() {
	a.withSecrets("Couldn't open the secrets", func(*secrets.Vault) error {
		for _, p := range a.st.Panes {
			if p.Kind == kindSecrets {
				a.st.Focus = p.ID
				return nil
			}
		}
		a.next++
		a.addPane(Pane{ID: "p" + itoa(a.next), Title: "Secrets", Kind: kindSecrets}, nil, placement{})
		a.watchVault()
		return nil
	})
}

// putSecret keeps a new secret, or changes one.
func (a *app) putSecret(in PutSecret) {
	a.withSecrets("Couldn't save the secret", func(v *secrets.Vault) error {
		it := secrets.Item{Name: in.Name, User: in.User, Kind: in.Kind}
		if in.ID != "" {
			items, err := v.Items()
			if err != nil {
				return err
			}
			i := slices.IndexFunc(items, func(it secrets.Item) bool { return it.ID == in.ID })
			if i < 0 {
				return secrets.ErrNoSuchItem
			}
			it = items[i]
			it.Name, it.User = in.Name, in.User
			if in.Value == "" {
				_, err := v.PutDetails(it)
				return err
			}
		}
		_, err := v.Put(it, in.Value)
		return err
	})
}

// onSecret runs do on a secret's item and value, in an open vault.
func (a *app) onSecret(id, what string, do func(it secrets.Item, value string)) {
	a.withSecrets(what, func(v *secrets.Vault) error {
		items, err := v.Items()
		if err != nil {
			return err
		}
		i := slices.IndexFunc(items, func(it secrets.Item) bool { return it.ID == id })
		if i < 0 {
			return secrets.ErrNoSuchItem
		}
		value, err := v.Secret(id)
		if err != nil {
			return err
		}
		do(items[i], value)
		return nil
	})
}

// copySecret puts a secret on the clipboard, which the window clears
// in half a minute unless something else has been copied since.
func (a *app) copySecret(id string) {
	a.onSecret(id, "Couldn't copy the secret", func(it secrets.Item, value string) {
		a.notices++
		a.st.Notices = append(a.st.Notices, Notice{ID: a.notices, Title: it.Name + " copied",
			Body: fmt.Sprintf("The clipboard clears in %d seconds.", clipboardHolds), Clipboard: value, Forget: true})
		a.copied, a.copiedAt = value, time.Now()
	})
}

// takeSecretBack clears the clipboard as the window closes, when it
// still holds a secret copied less than clipboardHolds ago, as gridterm
// does: the window's own clearing would never come.
func (a *app) takeSecretBack() {
	if a.copied == "" || time.Since(a.copiedAt) > clipboardHolds*time.Second {
		return
	}
	if now, err := readClipboard(); err == nil && now == a.copied {
		if err := writeClipboard(""); err != nil {
			log.Printf("taking the secret off the clipboard: %v", err)
		}
	}
	a.copied = ""
}

// readClipboard and writeClipboard reach the system's clipboard from
// the program's side. A test puts its own in their place.
var (
	readClipboard  = clip.Text
	writeClipboard = clip.SetText
)

// typeSecret types a secret into the terminal used last, as if pasted.
func (a *app) typeSecret(id string) {
	a.onSecret(id, "Couldn't type the secret", func(it secrets.Item, value string) {
		sh := a.shells.get(a.lastTerminal)
		if sh == nil || a.kindOfPane(a.lastTerminal) != kindTerminal {
			a.notify("No terminal to type into", "Click into a terminal first, then type the secret from here.", "")
			return
		}
		// A pane an agent asked for a secret on is waiting for the
		// whole answer, return and all.
		if sh.t.AskedForASecret() {
			value += "\r"
		}
		sh.t.Paste(value)
		a.st.Focus = a.lastTerminal
	})
}

// revealSecret shows a secret in a dialog, for reading off.
func (a *app) revealSecret(id string) {
	a.onSecret(id, "Couldn't show the secret", func(it secrets.Item, value string) {
		if value == "" {
			value = "Nothing is saved under this name."
		}
		go func() { _, _ = a.ask(a.ctx, Ask{Title: it.Name, Text: value, Yes: "Done", Plain: true}) }()
	})
}

// kindOfPane returns the kind of pane id.
func (a *app) kindOfPane(id string) string {
	for _, p := range a.st.Panes {
		if p.ID == id {
			return p.Kind
		}
	}
	return ""
}

// lockSecrets locks the vault. What is on disk stays.
func (a *app) lockSecrets() {
	if a.secrets != nil {
		a.secrets.Lock()
	}
	a.showVault()
}

// Intents for what opens the secrets.
type (
	// AddSecretsKey lets another key open the secrets, asking which.
	AddSecretsKey struct{}
	// RemoveSecretsKey stops a key, or the passphrase, opening the
	// secrets.
	RemoveSecretsKey struct{ Fingerprint string }
	// AddSecretsPassphrase lets a passphrase open the secrets too.
	AddSecretsPassphrase struct{ Passphrase string }
)

// addSecretsKey asks which key to add, of those in ~/.ssh that do not
// open the secrets yet, and adds it once unlocked.
func (a *app) addSecretsKey() {
	a.withSecrets("Couldn't add the key", func(v *secrets.Vault) error {
		have := map[string]bool{}
		for _, s := range v.Keys() {
			have[s.KeyFile] = true
		}
		var spare []string
		for _, k := range a.knownKeys() {
			if !have[k] {
				spare = append(spare, k)
			}
		}
		if len(spare) == 0 {
			a.notify("No key to add", "Every ed25519 key in ~/.ssh opens the secrets already. A key from another machine has to be copied here first.", "")
			return nil
		}
		go a.chooseKeyToAdd(v, spare)
		return nil
	})
}

// chooseKeyToAdd asks which of spare to add, warns about it when
// there is reason to, and adds it. It runs on a goroutine of its own.
func (a *app) chooseKeyToAdd(v *secrets.Vault, spare []string) {
	q := Ask{Title: "Add Secrets Key", Text: "Choose the key that opens the secrets too, such as another machine's."}
	for _, k := range spare {
		q.Choose = append(q.Choose, filepath.Base(k))
	}
	ans, err := a.ask(a.ctx, q)
	if err != nil || !ans.Yes {
		return
	}
	i := slices.Index(q.Choose, ans.Answers[len(ans.Answers)-1])
	if i < 0 {
		return
	}
	keyFile := spare[i]
	if warn := warningsAboutKey(a.ring, v, keyFile); warn != "" {
		ans, err := a.ask(a.ctx, Ask{Title: "Add " + keyFile + "?", Text: warn, Yes: "Add", Danger: true})
		if err != nil || !ans.Yes {
			return
		}
	}
	signer, err := a.ring.Unlock(a.ctx, keyFile, newAsker(a, ""))
	a.events <- func() {
		if err == nil {
			err = v.AddKey(signer, keyFile)
		}
		switch {
		case errors.Is(err, errDeclined), errors.Is(err, context.Canceled):
		case err != nil:
			a.notify("Couldn't add the key", err.Error(), "")
		default:
			a.notify("Key added", fmt.Sprintf("%s opens the secrets. %d ways in now.", keyFile, len(v.Keys())), "")
		}
		a.showVault()
	}
}

// removeSecretsKey stops a key opening the secrets. The last way in
// stays: without it, nothing would open them.
func (a *app) removeSecretsKey(fingerprint string) {
	a.withSecrets("Couldn't remove the key", func(v *secrets.Vault) error {
		if len(v.Keys()) < 2 {
			a.notify("Only one key opens the secrets", "Add another first, so something still opens them.", "")
			return nil
		}
		return v.RemoveKey(fingerprint)
	})
}

// addSecretsPassphrase lets a passphrase open the secrets. Working the
// passphrase into a key takes a moment on purpose, so it runs on a
// goroutine of its own.
func (a *app) addSecretsPassphrase(pass string) {
	a.withSecrets("Couldn't add the passphrase", func(v *secrets.Vault) error {
		if v.TakesAPassphrase() {
			a.notify("A passphrase opens the secrets already", "Remove it first to set another.", "")
			return nil
		}
		go func() {
			err := v.AddPassphrase(pass)
			a.events <- func() {
				if err != nil {
					a.notify("Couldn't add the passphrase", err.Error(), "")
				} else {
					a.notify("Passphrase added", "It opens the secrets where none of their keys is.", "")
				}
				a.showVault()
			}
		}()
		return nil
	})
}

// whatRemovingCosts says what is left once a slot goes.
func whatRemovingCosts(v *secrets.Vault, s secrets.KeySlot) string {
	for _, other := range v.Keys() {
		if other.Fingerprint != s.Fingerprint && onThisMachine(other) {
			return "Another key on this machine still opens the secrets."
		}
	}
	if v.TakesAPassphrase() && !s.ByPassphrase() {
		return "The passphrase still opens them here."
	}
	return "Opening them here again needs a key from another machine."
}

// passphraseInHand is the passphrase the secrets keep for a key file,
// when a key already unlocked opens them, or "". It never asks: the
// key being unlocked may be the one the secrets need.
func (a *app) passphraseInHand(keyFile string) string {
	v, err := a.vault()
	if err != nil || !v.Exists() {
		return ""
	}
	if v.Locked() && v.Unlock(a.ring.Signers()) != nil {
		return ""
	}
	pass, err := v.PassphraseFor(keyFile)
	if err != nil {
		return ""
	}
	return pass
}

// waitingForSecret names the terminal used last when an agent has
// asked for a secret there, or is "".
func (a *app) waitingForSecret() string {
	if t := a.terminal(a.lastTerminal); t != nil && t.AskedForASecret() {
		return a.titleOf(a.lastTerminal)
	}
	return ""
}
