# Plan: a pane for the secrets

Asked for on 23 September, after the secrets manager landed as commands
and dialogs alone. Editing or removing one is a palette command, and the
list it opens goes away as soon as anything is picked from it, so doing
three things means asking three times and never seeing the whole.

What it is: a pane that lists what is in the vault, lets it be worked
on, and lets it out again. The last part is not a nicety. A password
manager nobody can leave is one nobody should adopt, and a way out is
what makes trying this safe.

Each step is one commit of the work, then a read-only adversarial review
of that diff, then a commit of the fixes. That is how the feature was
built and it found two rounds of real bugs, including two that lost
data.

## What Marcus settled, 23 September

1. **A pane, for an overview and for working in.** Edit, delete and the
   rest, without a dialog that vanishes on the first pick.
2. **A way out, and it is not optional.** Whoever keeps passwords here
   can take them to another manager whenever they like.

## What stays a dialog, and why

Not everything moves. The split is between *using* a secret and
*managing* one, and the two want opposite things.

**Using one wants a modal.** `Type` sends a secret to the program in
the pane in front, which the chooser knows because it is drawn over
that pane: `focusedTerminal()` is the terminal you came from. A pane has
the focus itself, so it does not know. That is not a detail to route
around -- the whole "the pane is waiting for one" path, which answers an
agent's `ask_for_secret`, rests on it. `Show Secrets` stays as it is.

**Managing them wants a pane.** An overview, several changes in a row,
and the thing still on screen afterwards.

So there are two ways in and they do different jobs. `Show Secrets…`
for use, `Manage Secrets` for the rest.

## Two things the pane makes load-bearing

**It must never be shared.** `handPane` and `windows.watching` both take
a `*term.Terminal`, so a pane that is not a terminal cannot be handed to
an agent or to a window that took this one over. That is true today and
it is true by accident of a type signature. With secrets on screen it
becomes a property worth stating, so the pane refuses in its own code
rather than resting on someone else's parameter type.

**It must empty when the vault locks.** Every dialog today is gone
before `Lock SSH Keys` can matter. A pane is not, so it has to watch for
the lock and clear itself -- names included, not just values.

## Step 1 -- the pane, read only

- `conns.Secrets` joins the kinds, so the pane has a row on the sidebar
  and a way back to it.
- `secretspane.go`, modelled on `logpane.go`, which is the smaller of
  the two panes already written.
- It lists what `Items()` gives: the name, who or what it is for, the
  kind, and when it was last changed. No value is ever drawn.
- It lists the key slots underneath: where each key was, whether it is
  on this machine, and whether its passphrase is in here. Today that is
  two commands and a chooser each, and it is the part of the feature
  most likely to go wrong quietly -- three of the bugs fixed this week
  were in it.
- Locking empties it. Sharing refuses it.
- Command `Manage Secrets`, no `…`: it opens the pane and asks nothing.

**Reading.** Read only is a real step, not a stub. It is the overview
that was asked for, and everything after it is editing what it shows.

## Step 2 -- working on a row

- Rename, change who it is for, generate a new value, remove.
- Several rows at once for removal, which is the one thing that is
  tedious a row at a time.
- The vault API this needs already exists: `PutDetails`, `Put`, `Remove`.
  Nothing new goes into the `secrets` package.

**Reading.** `refresh()` means another window's changes arrive before
each of these, so a row the pane is showing may be gone by the time it
is acted on. The pane has to say that rather than report a failure that
reads like a bug.

## Step 3 -- the keys, from the pane

- Add and remove key slots without leaving it.
- The two questions before a key is trusted (in the SSH agent, its
  passphrase in here) are already written and already right: they are
  reused, not rebuilt.

## Step 4 -- the way out

This is the step the whole thing is for, so it is worth being exact
about what it is and is not.

**What export is for.** Not backup: the vault file is already a backup
and it is encrypted, so copying it is better than anything this could
write. Not moving machines: `Add Secrets Key` and a copy of the file
already do that. It is for **leaving gridterm**, and that means
plaintext, because that is what every other manager reads.

**So it is deliberate.** One file the user names, mode `0600`, and one
question before it is written. That question earns rule 10 outright --
it is the one action in the feature that takes every secret out of the
thing built to hold them:

```
Write every secret to <path>?
Anyone who can read the file can read them all.
```

`Write` and `Cancel`, opening on `Cancel`.

No export to the clipboard. No default path, so it is never one press
from happening. No writing over a file that is there.

**The format is CSV**, because 1Password, Bitwarden and KeePass all read
it. The columns are the common ones -- name, username, password, notes
-- with `kind` and `file` on the end so gridterm can read its own export
back without losing what it knows. Importers ignore columns they do not
recognise.

**Key passphrases go in it.** They are secrets the user owns, and a way
out that quietly keeps some back is not one. They are the rows where
`kind` says `passphrase` and `file` names the key.

## Step 5 -- the way in

- CSV from the same managers, mapped back onto name, for, value.
- Import is the easy direction: the plaintext file is already on their
  disk and this moves it into safety.
- The dialog says the file is still there afterwards. Whoever exported
  from another manager to get here has a file full of passwords in their
  downloads and may not have thought about it.

**Reading.** Duplicates need an answer -- skip, replace, or keep both.
Keep both is the safe default for a password manager: nothing a user
already has should disappear because a file said so.

## Still to settle

1. **Does `Show Secrets…` keep `Show`?** With a pane to read them in,
   the chooser could be Type and Copy alone, and reading one could be
   the pane's job.
2. **An encrypted export as well as the plaintext one?** It would make
   export a backup too. Against it: the vault file already is one, and a
   second encrypted format is a second thing to open in five years.
3. **How much editing before this is worth landing?** Step 1 and 2 are
   a usable feature on their own; 4 and 5 are what make it safe to
   adopt.

## What this is likely to cost

`jobpane.go` is 878 lines for something simpler than this. With editing,
several rows at once, and two file formats, the pane is the largest
single piece of the secrets feature so far -- bigger than the vault. The
steps are ordered so that stopping after any one of them leaves
something whole.
