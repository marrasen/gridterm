# Every dialog in gridterm — rewrite

A change sheet against `Every_dialog_in_gridterm.md`, matched on the
D-ids. Each entry gives the on-screen text as it should read after the
change, in full, so nothing has to be inferred from a diff. `Code:`,
`Opened from:` and `Notes:` lines are not repeated; they stay as they
are unless an instruction says otherwise. Focus stays where it is
unless stated.

`Body: none` means the dialog has no body lines at all.

---

# House rules

These are what every entry below was written to. They are also in
[WORDING.md](WORDING.md), which is where a new dialog should be written
from: this file is the change sheet, that one is the standing rule.

1. **Say what it is. Never what it is not.** No "not a path", "rather
   than", "nothing else", "nothing changes", "not what the shell ran".
2. **Use the words software uses.** Lost, invalid, failed, not found,
   cancel, retry, create, remove, save, reload. A program does not
   "go", "give up", "let go", "hold", "forget", "take", "kick out" or
   "say something else".
3. **The title is the message.** The body adds only what the title
   cannot: which machine, which path, one consequence. If the title
   says it all, there is no body.
4. **Never explain the program's reasoning.** The user needs the
   choice, not why gridterm has to offer it.
5. **Buttons are one verb**, from a small fixed set: `OK` `Cancel`
   `Close` `Retry` `Wait` `Save` `Create` `Delete` `Remove` `Replace`
   `Skip` `Open` `Run` `Connect`. No pronouns: never `Make it`,
   `Close it`, `Keep them`, `Leave it`.
6. **`Cancel` cancels what the dialog is about. `Close` dismisses a
   dialog and leaves things running.** Same meaning everywhere (D04,
   D09, D34).
7. **Errors are two or three words, sentence case, no full stop:**
   `Invalid passphrase`, `Enter a command`.
8. **Placeholders are a format or the word `Optional`.** Never a
   sentence, never "what to call it".
9. **If a sentence exists to excuse a behaviour, change the
   behaviour.** Retry limits, fields that are silently ignored and
   toggle buttons are all fixed below rather than explained.
10. **A warning earns its place only where the action exposes
    something or cannot be undone**, and then it is one sentence
    stating the consequence (D08, D17, D29, agent forwarding in D11).

## Vocabulary

| Was | Is |
|---|---|
| went, goes, gone | lost, closed, stopped, not found |
| give up | cancel |
| let go of | disconnect from |
| forget / forgotten | remove |
| remember / kept | save / saved |
| make | create |
| reread | reload |
| chord | shortcut |
| picture | image |
| things | items |
| hand-over | share |
| take (a menu line) | choose |
| through (a server) | jump host |

---

# Global instructions

- **G1. `Copy` on a Notice only when there is something to copy:** a
  path, an error, a log, a key. Give `ui.Notice` a flag. A notice that
  says "done" has `OK` alone.
- **G2. Success is not a dialog.** When an action worked and there is
  nothing to read, show one line in the status line and no dialog.
  Applies to D41 and D46.
- **G3. Cycling fields explain themselves.** `ui.Form` draws
  `Ctrl+↑/↓` beside a cycling field while it has focus. Delete every
  body sentence about stepping, choosing, ticking with space, or
  clearing a box.
- **G4. Per-field hints.** A field may carry one hint line, drawn at
  the bottom of the form only while that field has focus. This
  replaces the paragraph-per-field bodies (D11, D19).
- **G5. Fields that do not apply are disabled**, not accepted and then
  explained away on save (D11).
- **G6. A dialog title matches the menu row that opens it**, minus the
  ellipsis. Menu rows that change as a result are listed at the end.

---

# 1. Connecting to machines

## D01
- **Title:** `Connect to server`
- **Body:** none
- **Fields:** `Server` — placeholder `[user@]host[:port]`
- **Buttons:** `Connect` · `Cancel`

## D02
- **Title:** `Connect to window`
- **Body:**
  ```
  The other window must be serving, with this
  machine's public key in its authorized_keys.
  ```
- **Fields:**
  - `Host` — placeholder `host[:<servePort>]`
  - `Key file` — placeholder `Optional`
- **Buttons:** `Connect` · `Cancel`

## D03
- **Title:** `Connection lost`
- **Body:** `<name>` — and, when there is a reason, `<why>` on a second line
- **Buttons:** `Reconnect` · `Close`

**Instructions:** Same shape as D06: the title says what, the body says
which.

## D04
- **Title:** `Already connecting to <name>`
- **Body:** none
- **Buttons:** `Wait` · `Retry` · `Cancel`
- **Focus:** `Wait`

**Instructions:** `Wait` joins the attempt in progress. `Retry` cancels
that attempt and dials again. `Cancel` closes the dialog and drops this
request; the attempt in progress carries on. I used `Cancel` where you
said `Abort`: Abort reads as "kill the connection attempt", which is
what Retry does, and Cancel already means "close this, change nothing"
in every other dialog. Swap the word if you still prefer it.

## D05
- **Title:** `Unlock private key`
- **Body:** the key's path
- **Fields:** `Passphrase` — masked
- **Buttons:** `Unlock` · `Cancel`
- **Error:** `Invalid passphrase`

**Instructions:** Remove the retry limit and all three counting
variants. The key file is on the local disk, so a limit protects
nothing. The dialog stays open, the field is cleared, and the user
types again or presses Cancel. I kept the word passphrase because D06
asks for the account password, and the two words are the only thing
telling the user which secret is wanted.

## D06
- **Title:** `Password`
- **Body:** `<user>@<host>`
- **Fields:** `Password` — masked
- **Buttons:** `Sign in` · `Cancel`
- **Error:** `Invalid password`

**Instructions:** Show the error when the server rejects the password
and asks again. No count here either; the server decides when it has
had enough, and that arrives as a connection error.

## D07
- **Title:** `Authentication`
- **Body:** `<user>@<host> asks:` then the server's text
- **Fields:** as today
- **Buttons:** `OK` · `Cancel`

**Instructions:** gridterm's own line stays first, for the reason in
the Notes.

## D08
- **Title:** `Unknown host key`
- **Body:**
  ```
  <addr> is not in known_hosts.

  <keytype>  <fingerprint>

  Verify the fingerprint before connecting.
  ```
- **Buttons:** `Connect` · `Cancel`
- **Focus:** `Cancel`

## D09
- **Title:** `Waiting for server`
- **Body:** `<user>@<host>:` then the server's lines, then
  `Continues automatically when you are done.`
- **Buttons:** `Open link` *(only with exactly one URL)* · `Copy` ·
  `Close` · `Cancel` *(Cancel only when the connection can be stopped)*
- **Focus:** `Close`

**Instructions:** This dialog usually carries a sign-in link, so it
needs `Copy`, which copies the server's lines. With that, the sentence
about the pane is dropped. `Close` dismisses the dialog and keeps
waiting. `Cancel` stops the connection.

`Open link` is drawn only when the server's lines hold exactly one URL.
With none or several, the button is absent and `Copy` does the job.

- **What counts as a URL:** `http://` or `https://` only, found in the
  text after `serve.Plain` has stripped it. Trailing `.` `,` `)` `>` and
  quotes are trimmed, and the opening bracket or quote with them. No
  other schemes, ever: the text comes from the server, and `file:` or a
  custom protocol handler must not be reachable from it.
- **Action:** hands the URL to the system's default browser. The dialog
  stays open, since the connection continues by itself and the dialog
  closes when it settles.
- **Never opened automatically.** Only on the button press.
- **Focus stays on `Close`**, so a stray Enter cannot launch a browser at
  an address the server chose.
- **The body is unchanged**, so the URL stays visible before the button
  is pressed.
- **On failure:** a D57 notice titled `Could not open the link`.

The same address written twice counts as one: the button still means one
thing. `onlyLink` in `ask.go` is where this lives.

## D10 — gone

The account of how a machine was reached is a pane now, not a dialog:
`Connection Log` opens it, and so does clicking the machine's row. An
account is read, scrolled and copied into a bug report, and a box that
has to be dismissed is the wrong shape for all three. The pane is the
window log's — a terminal like any other, with nothing to type into it.

The row of a connection that dropped opens the same pane, which is where
the reason it went is written: it used to be squeezed onto the row as a
note.

---

# 2. The saved server list

## D11
- **Title:** `Add server`, or `Edit <name>`
- **Body:** none
- **Fields:**
  - `Name` — no placeholder
  - `Type` — cycles `SSH` / `gridterm window`
  - `Server` — placeholder `[user@]host[:port]`
  - `Key file` — placeholder `Optional`
  - `Jump host` — placeholder `Optional`
    - hint: `Connect through another saved server`
  - `Folders` — placeholder `Comma-separated paths`
    - hint: `Where the file browser opens on this server`
  - `Shell setup` — cycles `No` / `Yes`
    - hint: `Tracks the directory and where each command ends. bash and zsh only.`
  - `Forward SSH agent` — cycles `No` / `Yes`
    - hint: `The server can use your keys for onward connections. So can root on the server.`
- **Buttons:** `Save` · `Remove` *(edit only)* · `Cancel`

**Instructions:** The whole body goes; G3 and G4 replace it. When Type
is `gridterm window`, disable `Jump host` and `Forward SSH agent` and
ignore any `user@`. Delete both lines that are added on save. Disable
`Jump host` when no other server is saved, and delete the sentence
that says so.

## D12
- **Title:** `Remove <name>?`
- **Body:**
  - connected, to a machine or a window:
    `<name> is connected. Removing it closes the connection and its panes.`
    — with things reached through it:
    `Removing it closes the connection and everything through it: <what>.`
  - still connecting: `Removing it cancels the connection in progress.`
  - nothing open: none
- **Buttons:** `Remove` · `Cancel`
- **Focus:** `Cancel`

---

# 3. SSH keys

## D13
- **Title:** `New SSH key`
- **Body:**
  ```
  Creates an ed25519 key pair.
  The public key is saved as <file>.pub.
  ```
- **Fields:**
  - `File` — pre-filled; placeholder `Private key path`
  - `Comment` — placeholder `Optional`
  - `Generate passphrase` — a tick box, drawn only where there is a
    vault to keep one in; hint `Saved in the secrets and used
    automatically`
  - `Passphrase` — placeholder `Optional`, masked
  - `Confirm passphrase` — masked, no placeholder
- **Buttons:** `Create` · `Cancel`
- **Error:** `Passphrases do not match`

**Instructions:** The tick sits above the two fields it turns off, so
it is read before a passphrase has been typed into one of them. Ticked,
both are disabled and cleared (G5): gridterm makes the passphrase, locks
the key with it, and puts it in the vault, and nobody is shown it. The
tick is absent, not disabled, where there is no vault, because a
disabled field never gets the focus its hint is drawn for.

## D14
- **Title:** `Key created`
- **Body:**
  ```
  Private key:  <path>
  Public key:   <path>.pub

  The passphrase is saved in the secrets.

  <the public key line>

  To install it on a server:
    ssh-copy-id -i <path>.pub user@host
  ```
- **Buttons:** `Copy public key` · `Copy` · `OK`

**Instructions:** No sentence about where the command runs. The action
button copies the public key line alone, which is the one thing anyone
wants from this dialog.

The passphrase line is drawn only when D13's tick was on, and it earns
its place by rule 10: the passphrase was never on screen and cannot be
typed, so this is the one place the user learns the window is what opens
this key from now on. The passphrase itself is not shown here or
anywhere else; it is an item in the vault like any other.

---

# 4. Tunnels

## D15
- **Title:** `Tunnel via <host>`
- **Body:** none
- **Fields:**
  - `Direction` — cycles `Local — listen here` /
    `Remote — listen on <host>`
  - `Listen on` — placeholder `[address:]port`; cycles the saved tunnels
  - `Forward to` — placeholder `host:port`
  - `Save this tunnel` — tick box, off
- **Buttons:** `Open` · `Cancel`
- **Focus:** `Open`

**Instructions:** Direction moves from the buttons into a field. Local
and Remote are the words every SSH client uses, the two options
describe themselves, and the body that had to explain the buttons is no
longer needed. It also makes D15 and D16 end the same way: `Open` ·
`Cancel`. Picking a saved tunnel sets the direction too.

## D16
- **Title:** `SOCKS proxy via <host>`
- **Body:** `A local SOCKS port. Connections go out from <host>.`
- **Fields:** `Listen on` — placeholder `[address:]port`
- **Buttons:** `Open` · `Cancel`

## D17
- **Title:** `Open <address:port> to the network?` — or
  `Open a port on <host> to the network?`
- **Body:**
  - tunnel: `Anyone who can reach <where> on that port is connected to <target>, with no authentication.`
  - SOCKS: `Anyone who can reach <where> on that port can connect to anything <host> can reach, with no authentication.`
  - remote forward adds: `<host> chooses where it listens. With GatewayPorts on, that is its whole network.`
- **Buttons:** `Open` · `Cancel`
- **Focus:** `Cancel`

---

# 5. Running a command

## D18
- **Title:** `Run command on <where>`
- **Body:** none
- **Fields:**
  - `Command` — no placeholder; cycles the saved commands
  - `Directory` — placeholder `Optional`
  - `Save this command` — tick box, off
- **Buttons:** `Run` · `Cancel`
- **Error:** `Enter a command`

---

# 6. Serving this window

## D19
- **Title:** `Serve this window`
- **Body:**
  ```
  A connected window can open shells, use the ones
  running, and read and write files as you.

  Allowed keys, from <authorized_keys path>:
    <one line per key>
  ```
- **Fields:**
  - `Port` — pre-filled; hint: `0 picks a free port`
  - `Listen on` — cycles `This machine only` / `All networks`
- **Buttons:** `Serve` · `Cancel`

**Instructions:** The text after the fields goes. `All networks` needs
no paragraph, and the Tailscale remark belongs in documentation.

## D20
- **Title:** `Serving this window`
- **Body:** the listening address, then `Connected:` with
  `  <name> from <addr>` per client, or `No one is connected.`
- **Buttons:** `Close` · `Disconnect <name>` *(or `Disconnect all`)* ·
  `Stop serving`
- **Focus:** `Close`

## D21
- **Title:** `Resume serving?`
- **Body:**
  ```
  Port:       <port, or "any free port">
  Listen on:  <where>
  ```
- **Buttons:** `Serve` · `Not now` · `Don't ask again`
- **Focus:** `Serve`

---

# 7. Agents

## D22
- **Title:** `Agent permissions`
- **Body:** `The agent can read this pane and type into it. Changes apply immediately.`
- **Fields** (tick boxes):
  - `Restart the connection`
  - `Open more panes`
  - `Read only`
  - `Read cleared scrollback`
- **Buttons:** `Close` · `Remove pane`
- **Focus:** `Close`

**Instructions:** The paragraph about where the code lives goes.
`Close` because nothing is pending; every box has already applied.

## D23
- **Title:** `Agent share`
- **Body:**
  ```
  An agent with this code can read and type in the panes below.

    <code>
  ```
  and after the rows: `<shortcut> copies the code.`
- **Fields:** as today
- **Buttons:** `Copy prompt` · `Setup` · `Write skill` · `Close` ·
  `Stop sharing`
- **Focus:** `Copy prompt`

## D24
- **Title:** `Set up <host>`
- **Body:** that host's setup lines, and nothing after them
- **Buttons:** `<host.copyTitle()>` · `Close`

**Instructions:** The button already says what it copies, so the
sentence describing the button goes, and the one about the shortcut
with it. The paragraph about gridterm's own path goes too; the setup
lines simply use `gridterm`. Keep every `copyTitle()` to two words:
`Copy command`, `Copy config`.

## D25
- **Title:** `Skill written`
- **Body:** `<path>` then `Restart <host> to load it.`
  — or, when the location is unknown: `<path>` then
  `Copy it to where <host> reads skills from.`
- **Buttons:** `Copy` · `OK`

## D26
- **Title:** `Replace existing skill?`
- **Body:**
  ```
  <path>

  The existing skill has been edited.
  Replacing it discards those edits.
  ```
- **Buttons:** `Replace` · `Cancel`
- **Focus:** `Cancel`

**Instructions:** Focus moves to `Cancel`, in line with D12, D17, D29
and D32: the button that changes nothing.

## D27
- **Title:** `gridterm path not found`
- **Body:** `The prompt uses "gridterm" as the command. It works when gridterm is on the PATH.`
- **Buttons:** `OK`

## D28
- **Title:** `Typing history`
- **Body:** `Input as the agent sent it, edits included.` then the column
  of times
- **Buttons:** `Copy` · `OK`

---

# 8. Files

## D29
- **Title:** `Delete <name>?` — or `Delete <n> items?`
- **Body:** `<filesystem>: <path>` then `This cannot be undone.`
- **Buttons:** `Delete` · `Cancel`
- **Focus:** `Cancel`

## D30
- **Title:** `New directory`
- **Body:** `<filesystem>: <path>`
- **Fields:** `Name` — no placeholder
- **Buttons:** `Create` · `Cancel`

## D31
- **Title:** `Rename <name>`
- **Fields:** `Name` — pre-filled, no placeholder
- **Buttons:** `Rename` · `Cancel`

**Instructions:** An unchanged name closes the dialog and does nothing.
The error line goes.

## D32
- **Title:** `Replace <name>?`
- **Body:** `Existing <kind> on <filesystem>: <size>, modified <when>.`
- **Buttons:** `Replace` · `Replace all` · `Skip` · `Skip all` · `Cancel`
- **Focus:** `Skip`

**Instructions:** Dismissing the dialog stops the whole job today, with
no button that says so. Add `Cancel`, which stops the job, and map Esc
to it.

## D33
- **Title:** `Go to directory`
- **Fields:** `Path` — pre-filled, no placeholder
- **Buttons:** `Go` · `Cancel`
- **Error:** `Enter a path`

## D34 — a pane now, not a dialog

File work is watched in a pane, opened by its row on the sidebar. A copy
takes as long as it takes, and a box that has to be dismissed before
anything else can be done is the wrong shape for something to watch: it
covers the window it was opened from, it takes the keys, and it cannot
be left open beside the work that carries on.

What the pane shows, from the top: what it is doing and how many files
there are; a bar; how much has moved, how fast, how long it has been
going and how long is left; the last seconds drawn as a run; the names
it was given, ticked as it passes them; and, along the bottom, the same
choices the dialog had.

- **While running:** `Cancel` · `Close`
- **When finished:** `Repeat` *(copies only)* · `Save this copy` — a tick
  box *(copies only)* · `Close`
- **Focus:** `Close`, and it moves there by itself when a job finishes,
  because `Repeat` is drawn where `Cancel` was.

The `Remember` / `Forget` button that changed its name is the tick box,
matching `Save this tunnel` and `Save this command`. Ticked means it is
in Saved Copies.

`Close` takes the pane away and leaves the work running, the way closing
any pane does not end what is behind it.

## D35
- **Title:** `Copied <n> files to <dir> on <machine>`
- **Body:** none — or `<n> failed. Details are on their rows.`
  — or `Already exists.`
- **Buttons:** `Copy` · `OK`

## D36
- **Title:** `File copied`
- **Body:** `<path> on <machine>`
- **Buttons:** `Copy` · `OK`

## D37
- **Title:** `Saved copies`
- **Body:** `None saved. A finished copy has a "Save this copy" box.`
- **Buttons:** `OK`

## D38
- **Title:** `Saved copies`
- Rows and `×` as today.

---

# 9. Looks and settings

## D39
- **Title:** `Theme`
- **Rows:** the theme in use is noted `current`

## D40
- **Title:** `Theme file created`
- **Body:**
  ```
  <path>

  A copy of <theme>. Edit it, then choose
  Options › Reload › Themes.
  ```
- **Buttons:** `Copy` · `OK`

## D41
**Instructions:** No dialog (G2). Status line: `Shortcuts reloaded`.
If a dialog has to stay, it is the title `Shortcuts reloaded`, no
body, `OK`.

## D42
- **Title:** `Shortcuts file created`
- **Body:**
  ```
  <path>

  Contains every current shortcut. Edit it, then
  choose Options › Reload › Shortcuts.
  ```
- **Buttons:** `Copy` · `OK`

**Instructions:** The two paragraphs on how overrides work move into
the file itself, as a comment block at the top. That is where someone
editing the file is looking when they need them.

## D43
- **Title:** `File locations`
- **Body:** one line per file, with these labels: `Settings`,
  `Saved servers`, `Themes`, `Shortcuts`, `Authorized keys`,
  `Known windows`, `Serving key`. Then the `~/.ssh` note cut to one
  line. Then either `Portable: files are kept beside gridterm.` or
  nothing.
- **Buttons:** `Make portable` *(same condition as today)* · `Copy` · `OK`
- **Focus:** `OK`

**Instructions:** "A copy that carries its own files" is portable mode,
and that is the word people search for. The three steps go, since the
button does them.

## D44
- **Title:** `Made portable`
- **Body:** one line per action: `Created <dir>`, `Copied <file>`
- **Buttons:** `Copy` · `OK`

## D45
- **Title:** `Terminal identity`
- **Fields:** `TERM_PROGRAM` — as today
- **Body:**
  ```
  Programs read TERM_PROGRAM to identify the terminal.
  Blank reports gridterm.

  Another name can enable features such as inline images.
  It can also produce sequences that gridterm shows as text.

  Applies to new panes.
  ```
- **Buttons:** `Save` · `Cancel`

## D46
**Instructions:** No dialog (G2). Status line:
`Shell setup on — applies to new panes`, or `off`. If a dialog has to
stay: title `Shell setup <on|off>`, body `Applies to new panes.`, `OK`.

## D47
- **Title:** `Shell not found`
- **Body:** `<shell> is no longer installed. The default shell was opened. Choose another under File › New Terminal In.`
- **Buttons:** `OK`

## D48
- **Title:** `WSL shells unavailable`
- **Body:** `Could not list the WSL distributions:` then `<error>`
- **Buttons:** `Copy` · `OK`

## D49
- **Title:** `Could not read some fonts`
- **Body:** `<error>`
- **Buttons:** `Copy` · `OK`

## D50
- **Title:** `Could not read some fonts`
- **Body:** `While looking for a fallback for a missing character:` then `<error>`
- **Buttons:** `Copy` · `OK`

---

# 10. The window itself

## D51
- **Title:** `Exit gridterm?`
- **Body:** `Still open: <list>.` — the last item reads `an agent share`
- **Buttons:** `Exit` · `Cancel`
- **Focus:** `Cancel`

## D52
- **Title:** `About gridterm`
- **Body:**
  ```
  A GPU-rendered terminal emulator for Windows.

  Version: v0.2.0
  ```
  The version is what the build calls itself: a tag for a release, and
  `dev-<commit>` for a build from a working tree.
- **Buttons:** `Check for updates` · `Copy` · `OK`
- **Focus:** `OK`
- **Notes:** `Copy`, because the version is the first thing a bug report
  needs. `Check for updates` opens D58 or D59, or says the answer on the
  bottom row when there is nothing to fetch.

## D53
- **Title:** `Keys and commands`
- Body and buttons as today.

## D54
- **Title:** `Split with`
- **Error:** `Window too small`

## D55
- **Error:** `Window too small`

## D56
No change.

## D57

**Instructions:** One shape only: `Could not <verb> <object>`. A
partial success is `<What worked>, but <what did not>`. Several titles
that differ only in internals merge, because the body carries the
error and the user cannot act on the difference.

```
A command could not be closed                      → Could not close the command
Could not clear that row                           → Could not clear the row
Could not close that pane                          → Could not close the pane
Could not close the connection the file was read through
                                                   → Could not close the file's connection
Could not close the pane                           (keep)
Could not close the pane that was connecting       → Could not close the pane
Could not close the reader                         → Could not close the file
Could not close the scrollback                     (keep)
Could not connect                                  (keep)
Could not connect to <name>                        (keep)
Could not copy to the clipboard                    (keep)
Could not finish <job>                             (keep)
Could not forget which shell to open               → Could not save the settings
Could not let go of <addr>                         → Could not disconnect from <addr>
Could not let go of a filesystem                   → Could not disconnect the filesystem
Could not let go of <name>                         → Could not disconnect from <name>
Could not make the directory                       → Could not create the directory
Could not open a terminal here                     → Could not open a terminal
Could not open <at>                                (keep)
Could not open it on <name>                        → Could not open on <name>
Could not open that tunnel's pane                  → Could not open the tunnel's pane
Could not open the tunnel                          (keep)
Could not paste                                    (keep)
Could not paste a picture into <name>              → Could not paste the image into <name>
Could not paste a picture onto <host>              → Could not paste the image to <host>
Could not read a directory                         → Could not read the directory
Could not read <name>                              (keep)
Could not remember what the serve dialog was set to → Could not save the settings
Could not remember what this hand-over allows      → Could not save the settings
Could not remember which agent this was for        → Could not save the settings
Could not remember which shell to open             → Could not save the settings
Could not rename it                                → Could not rename <name>
Could not run it on <where>                        → Could not run the command on <where>
Could not say what is being served                 → Could not show the serving status
Could not show what this window is serving         → Could not show the serving status
Could not start it again in this pane              → Could not restart the command
Could not take that file                           → Could not copy the file
Could not write the skill for <host>               (keep)
Go to                                              → Could not go to <path>
That could not be done                             → Operation failed
The colour themes could not be found               → Could not find the themes
The colour themes could not be read                → Could not read the themes
The font size could not be changed                 → Could not change the font size
The key was made and not added to the list         → Key created, but not added to the list
The key was made and this window cannot say where to install it
                                                   → Key created, but install steps are unavailable
The server list could not be read                  → Could not read the server list
The server was saved and its key was not added to the list
                                                   → Server saved, but its key was not added
The settings could not be read                     → Could not read the settings
The theme's typeface could not be read             → Could not read the theme's font
The tunnel <label> stopped                         → Tunnel <label> stopped
The window serving <name> was lost                 → Connection to <name> lost
This copy could not be given files of its own      → Could not make portable
Trouble closing <name>                             → Could not close <name>
```

The secrets headings were written to this shape rather than reworded
into it:

```
Could not open the secrets
Could not create the secrets
Could not save the secret
Could not change the secret
Could not remove the secret
Could not create the key
Could not add the key
Could not remove the key
Could not export the secrets
Could not import the secrets
Could not add the passphrase
```

A passphrase dialog the user closed is not one of these. It reports
nothing: they shut it, so they know, and a notice saying so is a second
dialog to dismiss for a thing they just did.

The bodies under these headings come from the `secrets` package, and
they say `the secrets` too. They used to say `the vault`, which put a
second name for one thing in front of whoever had just read the first.
The package keeps the word for its own types and comments; it is the
file on disk and the thing in memory, and neither is what the window
calls them on screen. Where the path already says which file, the noun
goes altogether -- `secrets: read %s` -- the way `settings: read %s`
does.

---

# 11. Secrets

The vault of passwords and notes. It lives in `secrets.json` beside
everything else the window saves, and an ed25519 key the user already
unlocks to reach a server is what opens it. Locking the SSH keys locks
it too.

The commands that open these are in the palette and on no menu.

D60 to D62 are what a window with no vault gets. The rest are drawn only
once the vault is open, so a passphrase dialog for its key (D05) may
come first; none of them says anything about that, because by the time
one is on screen the question has been answered.

## D60
- **Title:** `No key to lock the secrets with`
- **Body:** `An ed25519 key is needed. Choose "New SSH Key" to create one.`
- **Buttons:** `OK`
- **Opened from:** any secrets command, when there is no vault and no
  ed25519 key to start one on.

**Instructions:** No `Copy` (G1): there is nothing here to paste. The
command is quoted by the name the palette lists it under, and both come
from one constant so the two cannot drift.

## D61
- **Title:** `No secrets yet`
- **Body:** `<key file> will open them.`
- **Buttons:** `Create` · `Copy` · `OK`
- **Focus:** `OK`
- **Opened from:** any secrets command, when there is no vault and
  exactly one ed25519 key could start one.

**Instructions:** `Copy` stays because the body is a path. With more
than one key to choose from this is D62 instead: which key opens the
vault is the user's to decide, since it is the only one that will until
another is added.

`Create` goes through D70 when there is anything to say about the key.

## D62
- **Title:** `Choose a key` — or `Add Secrets Key` / `Remove Secrets Key`
  when one of those commands opened it
- **Rows:** one per ed25519 key file this list is offering
- **Opened from:** any secrets command, when there is no vault and
  several keys could start one; and `Add Secrets Key` and `Remove
  Secrets Key`, over their own sets of keys.

**Instructions:** Titled for the command under G6 and P4. Both key
lists said `Choose a key`, so two different jobs shared one heading and
nothing on screen said which of them the user was in. Only the list
that starts a vault keeps the plain name, because no command is called
that.

## D63
- **Title:** `Show Secrets`, or
  `Show Secrets — the pane is waiting for one`
- **Rows:** the name of each secret, noted with who it is for, the key
  file a passphrase opens, and the kind when it is not a password
- **Filter:** a row of its own, `type to narrow the list`
- **Buttons:** `Type` · `Copy` · `Show` · `Cancel`
- **Opened from:** `Show Secrets`

**Instructions:** Up and down pick the secret, left and right pick what
to do with it, Enter does it — the same keys as the buttons on any
other dialog.

`Type` sends the secret to the program in the pane in front, so it never
reaches the clipboard. A return goes with it only when something is
waiting for a whole answer, which is also when the title says so: a
password sent into an ordinary prompt with a return cannot be taken
back.

`Copy` puts it on the clipboard and says so on the bottom row without
showing it. `Show` is D64. No secret is ever drawn on a row.

The four are `Type` `Copy` `Show` `Cancel`, and all four are constants
in `wording.go` like every other button this window draws.

## D64
- **Title:** `<name>`
- **Body:** the secret, as it was saved
- **Buttons:** `Copy` · `OK`
- **Opened from:** `Show` on D63.

**Instructions:** Preformatted, because a recovery code in columns is
read wrong if the words are rewrapped to fit. This is the only dialog
that puts a secret on screen, and it takes two presses to reach: the
one thing the vault is for is not showing them by accident.

An item saved with nothing in it gets the body `Nothing is saved under
this name.` and `OK` alone.

## D65
- **Title:** `No secrets yet`
- **Body:** `Choose "Add Secret" to add one.`
- **Buttons:** `OK`
- **Opened from:** `Show Secrets`, `Change Secret` and `Remove Secret`,
  with an empty vault.

**Instructions:** One answer for all three. `Change Secret` had a
heading and a sentence of its own, `Secrets` over `There is nothing to
change yet.`, which said the same thing in different words and carried
a `Copy` button over a body with nothing in it to copy (G1).

## D66
- **Title:** `Add Secret`
- **Body:** `Only your key opens the secrets.`
- **Fields:**
  - `Name`
  - `For` — placeholder `Optional`, pre-filled with the machine in
    front of the user, offering the machines the window knows of; hint
    `Who or what the secret is for`
  - `Secret` — masked
  - `Show the secret` — a tick box, which turns the stars off
- **Buttons:** `Save` · `Generate` · `Cancel`
- **Opened from:** `Add Secret`

**Instructions:** The body says the one thing the title cannot: who can
read it back. That is nobody else, and somebody typing a password into
a window is owed it.

It says `the secrets`, because that is what every title, command and
line along the bottom calls them. The word `vault` belongs to the
package and to the file on disk; on screen it was a second name for one
thing, on the one dialog where a user meets it first.

`Generate` fills the field with twenty characters and leaves the dialog
open. What it writes stays masked; the tick is what reads it back, so
the two are one job each.

The tick was a button that said `Show` and renamed itself to `Hide`,
which rule 9 calls a bug wearing an explanation — and the explanation
was a comment saying it had to say what the next press did rather than
what the last one did. A box says which way it is without being read
twice, and it sits beside the field it is about rather than down among
the verbs.

`For` is a suggestion in a field the user can clear, not a decision. It
starts empty on a local pane, because there is no machine to name.

Saving says `<name> saved` on the bottom row (G2).

## D67
- **Title:** `Add Note`
- **Body:** `Only your key opens the secrets.`
- **Fields:** `Name`, `For`, `Note` — not masked
- **Buttons:** `Save` · `Cancel`
- **Opened from:** `Add Note`

**Instructions:** A note is a licence or a recovery code, read off the
screen as often as it is pasted, so it is shown as it is typed and there
is no tick to turn the stars off. It came from somewhere else, so there
is nothing to `Generate` either.

## D68
- **Title:** `Change Secret — <name>`
- **Fields:**
  - `Name` — filled in
  - `For` — filled in, offering the machines the window knows of
  - `New secret` — placeholder `Optional`, masked; hint `Leave empty to
    keep the saved one`
  - `Show the secret` — a tick box
- **Buttons:** `Save` · `Generate` · `Cancel`
- **Opened from:** a row of `Change Secret`, which is a list titled
  `Change Secret`.

**Instructions:** The value field starts empty and empty means keep it.
The saved secret is not put there to be edited: that would be a password
sitting on screen behind a row of stars, and changing a name would have
to read it. As it is, renaming one never reads it at all.

`Generate` is how a password is rolled over on a machine. For a note the
field is `New note`, unmasked, with neither `Generate` nor the tick.

Saving says `<name> changed` on the bottom row.

## D69
- **Title:** `No key to add`
- **Body:** `No other ed25519 key is on this machine. Choose "New SSH Key"
  to create one.` — or, when every key the window knows of already opens
  the vault, `Every ed25519 key this window knows of already opens the
  secrets. A key from another machine has to be on this one first.`
- **Buttons:** `OK`
- **Opened from:** `Add Secrets Key`, with nothing to offer.

**Instructions:** Two bodies because there are two reasons, and what the
user does next differs. With keys to offer this is D62, whose rows note
`passphrase in the secrets` for a key the vault holds the passphrase
of.

Adding says `<key file> opens the secrets — <n> keys do now` on the
bottom row.

## D70
- **Title:** `Add <key file>?` — or `Create the secrets on <key file>?`
  when there is no vault yet
- **Body:** one sentence per thing worth saying, and nothing else:
  - the key's passphrase is in this vault: `Its passphrase is in the
    secrets, so another key is still needed to open them.`
  - the SSH agent is holding the key: `A server you forward the agent
    to can open any copy of the secrets it has.`
- **Buttons:** `Add` · `Cancel` — or `Create` · `Cancel`
- **Focus:** `Cancel`
- **Opened from:** a row of D62, under `Add Secrets Key` and when
  starting a vault. A key with nothing against it gets no dialog.

**Instructions:** One dialog for both, because a key can have both
against it. The title names the action and the key the way D12, D17,
D29 and D72 do, so the body is the consequences and nothing else: two
of them are two sentences, one each, in that order.

Rule 10 covers both. A second key is added so that losing the first
does not lose the vault, and a key whose passphrase is in the vault
cannot do that job. And a slot is opened by the key signing, so
anywhere the agent can be reached is somewhere the secrets can be
opened.

Neither is refused. Both are only a cost to somebody in a particular
position — with the other key lost, or with a copy of the file — and
which of those is worth it is the user's to weigh. `Cancel` keeps the
focus, the way it does on every question about exposing something.

Rule 4 applies hardest here: the body says what it costs and never how
a slot key is made. The first draft explained the challenge and the
signature, which is gridterm's reasoning and not the user's choice.

The agent is asked by fingerprint, off the `.pub` file beside the key,
so nothing has to be unlocked to ask. A key with no `.pub`, or an agent
that will not answer, gets no warning: one this window cannot stand
behind is worse than none. It is a snapshot either way — the key may be
added to the agent a minute later.

The asking is held to a clock, and skipped when the window has already
given up on the agent once. This runs on the goroutine that draws, in
the moment between picking a key and being warned about it, so an agent
that takes the connection and then says nothing would be a window that
has stopped with no way out.

## D71
- **Title:** `Only one key opens the secrets`
- **Body:** `Choose "Add Secrets Key" to add another
  first.`
- **Buttons:** `OK`
- **Opened from:** `Remove Secrets Key`, with one key in the vault.

**Instructions:** With two or more this is D62, whose rows are the key
file — or the fingerprint, where the vault was never told where the key
was — noted `on this machine` and with the fingerprint.

## D72
- **Title:** `Remove <key file>?`
- **Body:** `Another key on this machine still opens the secrets.` — or,
  when it is the last one here, `Opening them here again needs a key
  from another machine.`
- **Buttons:** `Remove` · `Cancel`
- **Focus:** `Cancel`
- **Opened from:** a row of D62 under `Remove Secrets Key`.

**Instructions:** Rule 10: a key that is gone cannot be put back without
the key itself. Opens on the button that changes nothing.

`Cancel` rather than `OK`, which is what a notice with an action on it
draws: on a question about removing something, `OK` reads as agreeing
to it. That also drops a `Copy` button over a body with nothing in it
to copy (G1). D70 is the same shape, so the two questions about a key
answer alike.

One sentence either way. The secrets stay open until the window locks
them, so somebody who has just shut themselves out has a moment to put
the key back; that is why the body says what opens them here again
rather than what has been lost.

Removing says `<key file> removed — <n> keys still open the secrets` on
the bottom row.

## D73
- **Title:** `Export Secrets`
- **Body:** none
- **Fields:** `File` — placeholder `CSV file path`; hint
  `Comma-separated values, which other managers read`
- **Buttons:** `Export` · `Cancel`
- **Error:** `Enter a path`
- **Opened from:** `Export Secrets`

**Instructions:** The way out, and the reason the rest of this is worth
adopting: a password manager nobody can leave is one nobody should keep
passwords in.

Nothing is filled in, and that is what keeps this from happening by
accident -- there is no path until one is typed. Not the focus: a form
whose first job is to be typed into opens in its field, or everything
typed goes to a button and nowhere. The question that opens on the way
out is D74, after this.

## D74
- **Title:** `Export every secret to <path>?`
- **Body:** `Anyone who can read the file can read them all.`
- **Buttons:** `Export` · `Cancel`
- **Focus:** `Cancel`
- **Opened from:** `Export` on D73.

**Instructions:** Rule 10, and the plainest case of it in the window:
this is the one action that takes every secret out of the thing built
to hold them. The shape a tunnel already uses -- a form to fill in,
then a question naming what filling it in would do.

Plain text on purpose. It is what every other manager reads, and a way
out that only gridterm can read is not one.

## D75
- **Title:** `Secrets written`
- **Body:**
  ```
  <path>

  <n> secrets, in plain text.

  Import it as "Chrome" or "Other CSV".
  Remove the file once it has been imported.
  ```
- **Buttons:** `Copy` · `OK`
- **Focus:** `OK`

**Instructions:** Which importer to pick, because that is not
guessable: there is no standard CSV and the managers that read this one
read it as a browser's. And to take the file away, because it is the
one place every secret sits in the clear.

## D76
- **Title:** `Import Secrets`
- **Body:** none
- **Fields:**
  - `File` — placeholder `CSV file path`; hint `Comma-separated values,
    as another manager writes them`
  - `Duplicates` — cycles `Keep both` / `Skip` / `Replace`; hint
    `What to do with a secret that is already here`
- **Buttons:** `Import` · `Cancel`
- **Error:** `Enter a path`
- **Opened from:** `Import Secrets`

**Instructions:** No warning: this is the easy direction. The plaintext
file is already on the user's disk and this moves it into something
sealed.

`Keep both` is what the field starts on, because it is the answer that
loses nothing. A file is not a reason for something somebody already
has to disappear.

## D77
- **Title:** `Secrets read in`
- **Body:**
  ```
  <n> secrets read in.
  <n> were already here.

  <path>
  Remove the file: every secret in it is in plain text.
  ```
- **Buttons:** `Copy` · `OK`
- **Focus:** `OK`

**Instructions:** The second line only when something was passed over.
The file is named because it has not moved, and whoever exported it
from another manager to get here may not have thought about that since.

## D78
- **Title:** `Add Secrets Passphrase`
- **Body:** `Anyone with a copy of the secrets can try passphrases
  against them.`
- **Fields:** `Passphrase` and `Confirm passphrase`, both masked
- **Buttons:** `Add` · `Cancel`
- **Focus:** `Cancel`
- **Errors:** `Passphrases do not match`, `Enter a passphrase`
- **Opened from:** `Add Secrets Passphrase`

**Instructions:** Rule 10. Every other way into the vault is a key in a
file, and nothing can be tried against one; this is what somebody
typed. One sentence, in D17's shape, which is who can do what.

Asked twice because it is masked and because it is the thing that gets
the user back in years from now: a typo makes a way in nobody can find.

Opens on the way out. Nothing adds one of these -- it is a command, and
the weaker door stays shut unless somebody opens it on purpose.

## D79
- **Title:** `Unlock Secrets`
- **Body:** none
- **Fields:** `Passphrase` — masked
- **Buttons:** `Unlock` · `Cancel`
- **Error:** `Invalid passphrase`
- **Opened from:** any secrets command, when no key of the vault's is
  on this machine or the one it names will not open it, and a
  passphrase has been added.

**Instructions:** No body, and its own title rather than D05's. `Unlock
Secrets` over a field called `Passphrase` says which passphrase and
what it opens; a line under it repeating that is what rule 3 deletes.
The title is the only thing telling this apart from D05, and it is
enough.

Asked as often as it takes, the way D05 is: the file is on this machine
and this user can already read it, so a limit guards nothing.

## D80
- **Title:** `Remove <name>?` — or `Remove <n> secrets?`
- **Body:** `This cannot be undone.`
- **Buttons:** `Remove` · `Cancel`
- **Focus:** `Cancel`
- **Opened from:** `Remove` on the secrets pane.

**Instructions:** Rule 10 on the second count: the vault holds the only
copy of what is in it and nothing in the window can put one back. D29's
own sentence, because it is the same fact.

The first draft said `The vault holds the only copy of what is in it.`,
which says *vault* where every other line says *the secrets*, and
*holds*, which rule 2's list has a program not doing.

---

# The secrets pane

Not a dialog, and the text on it is governed all the same.

- **Heading:** `Manage Secrets`, or `Manage Secrets — <n> secrets`
- **Rows:** each secret's name, with who it is for, the key file a
  passphrase opens, and the kind when it is not a password
- **Under them:** `Keys that open them`, then one row per slot: the key
  file noted `on this machine`, or `Passphrase` noted
  `a way in without a key`
- **Buttons on a secret:** `Copy` · `Show` · `Change` · `Remove` ·
  `Add secret` · `Add note`
- **Buttons on a key:** `Add key` · `Remove key`
- **Empty:** `Nothing here yet. Choose "Add Secret" to add one.`
- **Locked:** `Locked. Choose "Show Secrets" to open them.`

**Instructions:** The buttons follow the row, the way a job pane's
follow its state, and answer the keys every list in this window
answers to: up and down pick the row, left and right pick what to do
with it, Enter does it. `Remove` says how many when several are ticked.

No value is ever drawn on a row. `Show` opens the notice the chooser
opens, which is read and then dismissed: a value revealed on a row
would sit there for as long as the pane did, and a pane outlives
everything.

A note that will not fit is dropped rather than trimmed. A note here is
a path or a fingerprint, and half a fingerprint is worse than none --
it can be held against another and believed to match.

---

---

# The two the update check opens

## D58
- **Title:** `Update available`
- **Body:**
  ```
  v0.2.0 is the newest release; this build is v0.1.0.
  https://github.com/marrasen/gridterm/releases/tag/v0.2.0
  ```
- **Buttons:** `Open` · `Copy` · `OK`
- **Focus:** `OK`
- **Opened from:** `Check for updates` on D52, when the newest release
  is later than this build.

## D59
- **Title:** `Newest release`
- **Body:**
  ```
  v0.2.0 is the newest release; this build is dev-3e62f4547e66.
  https://github.com/marrasen/gridterm/releases/tag/v0.2.0
  ```
- **Buttons:** `Open` · `Copy` · `OK`
- **Focus:** `OK`
- **Opened from:** `Check for updates` on D52, when this build is not a
  release and so has no order against one.

---

# Answers that are a line, not a dialog

`Check for updates` on D52 puts these on the bottom row through
`(*app).say`, because there is nothing to fetch and nothing to answer:

- `v0.2.0 is the newest release` — this build is that release.
- `This build is later than the newest release, v0.2.0` — what building
  from `main` gives.
- `Checking for updates…` — while the question is out.

The secrets commands answer the same way (G2), because each worked and
there is nothing to read:

- `A passphrase opens the secrets now`

- `Secrets created — <key file> opens them`
- `<name> saved`, `<name> changed`, `<name> removed`
- `<name> copied — the clipboard clears in 30 seconds`
- `<key file> opens the secrets — <n> keys do now`
- `<key file> removed — <n> keys still open the secrets`

The copy line says the number because the clipboard is emptied again
half a minute later, and a user who does not know that pastes nothing
and has no idea why. A clipboard they have used since is left alone.

---

# Menu rows that follow from this

So titles and menus keep matching (G6) and the vocabulary holds:

- `Forget This Server…` → `Remove This Server…`
- `Remembered Copies…` → `Saved Copies…`
- The Options row that opens D45 → `Terminal Identity…`
- The status-bar hints and palette names that still say reread,
  remember, forget or make should follow the vocabulary table too.
