package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/crypto/argon2"
)

// slotKindPassphrase is a slot opened by something the user knows
// rather than something they hold.
//
// Every other slot is an SSH key, which is the whole of how this vault
// is opened and the whole of how it is lost: a user whose keys are all
// gone has no way back in, and copying the file does not help, because
// the copy wants the same keys. A passphrase slot is the way back.
//
// It is a weaker door and it is meant to be the second one. An ed25519
// key is a hundred and twenty-eight bits in a file; a passphrase is
// what somebody typed, and anybody holding this file can guess at it
// for as long as they like with nothing to stop them but the cost of
// each guess. Nothing adds one. The user asks for it, and is told that
// before they do.
const slotKindPassphrase = "passphrase"

// What a guess costs. Argon2id, at the memory OWASP names for it.
//
// Written into the slot rather than assumed, so these can be raised
// later without shutting anybody out of a vault sealed under the old
// ones. A slot that names none is read with these: no file has ever
// been written without them, and the fallback is for the version that
// raises them and meets one written by this one.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
)

// ErrWrongPassphrase says a passphrase opened no slot.
var ErrWrongPassphrase = errors.New("secrets: that passphrase does not open the secrets")

// ErrNoPassphrase says nothing in this vault is opened by one.
var ErrNoPassphrase = errors.New("secrets: no passphrase opens the secrets")

// passKeyFrom derives a slot key from a passphrase.
//
// Argon2id, which is slow and wants a lot of memory on purpose: the
// whole strength of this slot is what one guess costs.
func passKeyFrom(pass string, s slot) []byte {
	time, memory, threads := s.Time, s.Memory, s.Threads
	if time == 0 || memory == 0 || threads == 0 {
		time, memory, threads = argonTime, argonMemory, argonThreads
	}
	return argon2.IDKey([]byte(pass), s.Salt, time, memory, threads, keyLen)
}

// AddPassphrase lets a passphrase open the vault, as a way back in when
// every key is gone.
//
// The vault has to be open already, the way adding a key needs it open:
// what is added is another wrapping of the data key, and the data key
// comes from having opened it.
func (v *Vault) AddPassphrase(pass string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return ErrLocked
	}
	if strings.TrimSpace(pass) == "" {
		return errors.New("secrets: a passphrase that is nothing opens the secrets to everybody")
	}
	v.refresh()
	s, err := wrapForPassphrase(pass, v.data)
	if err != nil {
		return err
	}
	was := slices.Clone(v.file.Slots)
	v.file.Slots = append(v.file.Slots, s)
	if err := v.save(); err != nil {
		v.file.Slots = was
		return err
	}
	return nil
}

// wrapForPassphrase builds a slot a passphrase opens, holding the data
// key.
func wrapForPassphrase(pass string, data []byte) (slot, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return slot{}, fmt.Errorf("secrets: create a salt: %w", err)
	}
	name := make([]byte, 8)
	if _, err := rand.Read(name); err != nil {
		return slot{}, fmt.Errorf("secrets: name a slot: %w", err)
	}
	s := slot{
		Kind:        slotKindPassphrase,
		Fingerprint: hex.EncodeToString(name),
		Salt:        salt,
		Time:        argonTime,
		Memory:      argonMemory,
		Threads:     argonThreads,
	}
	key := passKeyFrom(pass, s)
	defer wipe(key)
	nonce, box, err := seal(key, data, nil)
	if err != nil {
		return slot{}, err
	}
	s.Nonce, s.Wrapped = nonce, box
	return s, nil
}

// TakesAPassphrase reports whether any slot is opened by one, so the
// window knows whether to offer it.
func (v *Vault) TakesAPassphrase() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.file == nil {
		return false
	}
	return slices.ContainsFunc(v.file.Slots, func(s slot) bool {
		return s.Kind == slotKindPassphrase
	})
}

// UnlockWith opens the vault with a passphrase.
//
// Its own way in rather than another argument to Unlock: a key is
// offered and tried for nothing, and a passphrase has to be asked for
// first. Nothing asks unless a slot takes one.
func (v *Vault) UnlockWith(pass string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data != nil {
		return nil
	}
	if v.file == nil {
		return fmt.Errorf("secrets: there is nothing at %s yet", v.path)
	}
	took := false
	for _, s := range v.file.Slots {
		if s.Kind != slotKindPassphrase {
			continue
		}
		took = true
		key := passKeyFrom(pass, s)
		data, err := unseal(key, s.Nonce, s.Wrapped, nil)
		wipe(key)
		if err != nil {
			// The wrong passphrase for this slot. Another slot may take
			// it: a vault can have more than one, and the user is not
			// being asked which.
			continue
		}
		if err := v.readContents(data); err != nil {
			wipe(data)
			// The contents are one sealed blob every slot shares, so
			// another slot will not read them either.
			return err
		}
		v.data = data
		return nil
	}
	if !took {
		return ErrNoPassphrase
	}
	return ErrWrongPassphrase
}
