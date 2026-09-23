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

**Reading.** Removing goes straight to the question, not through a
list: the row the bar is on is which key, and a chooser in between
would ask again what the screen already says. The guard against taking
the last one is the chooser's own, lifted out so both call it.

## Somewhere taller to write a note

Not a step of its own, and worth writing down before it is forgotten.
`Add note` puts a note on one line, in the field a password uses with
the stars turned off. A note is a licence, a recovery code, half a
page of joining instructions -- the things people keep that are not one
word. One line is the wrong shape for all of them.

What it wants is a box several rows tall that wraps, in the form where
the field is now. That is a toolkit change rather than a secrets one:
`ui.Field` is one line by construction, and nothing else in the window
has ever needed more. Worth doing on its own, for whatever else grows a
paragraph later.

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
Export every secret to <path>?
Anyone who can read the file can read them all.
```

`Export` and `Cancel`, opening on `Cancel`.

Two dialogs, not one: a form for the path and then the question, which
is the shape a tunnel already uses. The first draft put the warning in
the form's body and opened that form on `Cancel`, and the form was
unusable -- everything typed went to a button and the field stayed
empty. A question with nothing to fill in opens on the way out; a form
whose first job is to be typed into opens in its field. What keeps the
export from happening by accident is that there is no path until one is
typed, which is a better guard than where the focus starts.

No export to the clipboard. No default path. No writing over a file
that is there: the file is made with `O_EXCL` and `0600` in one step,
so a path typed over something else cannot take it away and there is no
moment when the file exists and is readable by everyone.

**The format is CSV, and there is no standard one.** Checked on 23
September rather than assumed. Every manager defines its own columns:
KeePassXC into Bitwarden wants headers renamed by hand, 1Password wants
"All Fields" and "Include Column Labels" ticked before its own export
is readable. CSV is what everything takes, not what anything agrees on.

So the columns are the browser shape, `name,url,username,password`,
which is the closest thing to a common one: Chrome insists on `url`,
`username` and `password` as headers, and Bitwarden, Dashlane and the
rest import a Chrome CSV directly. Whoever leaves gridterm picks
"Chrome" or "Other CSV" in whatever they are moving to, and it works.
The dialog should say so, because that choice is not guessable.

`notes`, `kind` and `file` go on the end, so gridterm can read its own
export back without losing what it knows. Importers ignore columns they
do not recognise.

**Key passphrases go in it.** They are secrets the user owns, and a way
out that quietly keeps some back is not one. They are the rows where
`kind` says `passphrase` and `file` names the key.

They are also the reason CSV is right rather than merely available. A
passphrase for `~/.ssh/id_ed25519` is not a login: it has no URL and no
username, and no credential format models it. CSV has a notes column
and no opinion, which is what an awkward secret needs.

**The file is a hazard until it is gone.** The notice after a successful
export says to remove it once it has been imported. That is the advice
every manager gives about its own export and it is worth repeating,
because the file is the one place every secret sits in the clear.

## The standard that is arriving, and why not yet

The FIDO Alliance's **Credential Exchange Format** (CXF) became a
Proposed Standard in August 2025, written by Apple, Google, Microsoft,
1Password, Bitwarden and Dashlane, for exactly the reason above: to end
the inconsistent CSV files that were the only common option. It is JSON
and it is a real specification.

It is not what this should write yet, for two reasons. The protocol
half, CXP, is still being finished, so what has shipped is local
on-device transfer through platform APIs -- Apple in iOS and macOS 26,
Google on Android. A terminal emulator on Linux and Windows is not part
of that path. And third-party managers are still prototyping their side,
so a CXF file gridterm wrote today has little that would read it.

What follows for the work: write the export behind something that takes
a format, so a second one is a new writer and not a new feature. CXF is
where this goes when managers can read it, and the CSV stays either way,
because a way out that depends on the other end being modern is not one.

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

**The headers are read, not assumed.** There is no standard CSV, so the
header line says which column is which and the reader knows the names
each manager writes: Chrome and the browsers, Bitwarden, LastPass,
KeePassXC, 1Password. Matched without case, because they do not agree
on that either. A file with no password and no notes column is refused
rather than read as nothing.

**`Item` gains a `URL`.** Nothing in the window asks for one and nothing
draws it. It is here because every manager has the column, and a way out
that quietly drops it is a way out that loses the user's work: what came
in from Bitwarden goes back out to Bitwarden whole. The Change form
should grow a field for it, which is a small piece of work on its own.

**One save for the lot.** A file of two hundred logins is two hundred
writes otherwise, and a failure half way through leaves half of them in.

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
