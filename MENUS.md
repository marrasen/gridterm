# The menus

What the bar is now. Same notation as before: the `id:` in brackets is
the command, so a line can be followed wherever it moves next.

- `⟨header: Text⟩` is a dim caption over the group under it. It cannot
  be chosen and stepping down a menu goes straight past it.
- `✓` is a switch: the row draws a tick while what it turns on is on.
- `⟨built at run time⟩` is a block the window generates.

**The whole of what a line does is written along the bottom row of the
window while a menu is open.** The menus say the short half -- "Right"
under a "Split" header -- and the row says "Split right". It is a row
borrowed for the moment a menu is up, not a bar that costs a row of
every pane. The palette uses the whole title too.

---

## Menu bar order

1. `[menu: file]` **File**
2. `[menu: edit]` **Edit**
3. `[menu: view]` **View**
4. `[menu: pane]` **Pane**
5. `[menu: machine]` **Machine**
6. `[menu: servers]` **Servers**
7. `[menu: share]` **Share**
8. `[menu: options]` **Options**
9. `[menu: help]` **Help**

Smallest thing a command acts on to the largest: the text, what is
drawn, the pane, the machine the pane is on, the address book, other
windows and agents, and the program.

---

## `[menu: file]` File

- `[id: pane.open]` New Terminal
- ⟨header: New Terminal In⟩ *(only when there is more than one shell)*
- ⟨built at run time⟩ the shells this machine has
- ⟨header: Close⟩
- `[id: pane.close]` Pane — `Ctrl+Shift+W`
- `[id: sidebar.closeRow]` Selected Row
- `[id: conn.disconnect]` Machine
- `[id: conn.clearFinished]` All Finished
- ⟨separator⟩
- `[id: app.exit]` Exit

"Selected Row" rather than "Connection": that command closes whatever
the sidebar has highlighted, which is not the focused pane. Two rows
that looked like one ladder would have acted on two different things.

## `[menu: edit]` Edit

- `[id: edit.copy]` Copy — `Ctrl+Shift+C`, `Ctrl+Insert`
- `[id: edit.paste]` Paste — `Ctrl+Shift+V`, `Shift+Insert`
- `[id: edit.pasteImage]` Paste Image as File… — `Ctrl+Alt+V`
- ⟨separator⟩
- `[id: pane.scrollback]` Find in Scrollback…

"in Scrollback" because it opens the text in the file viewer, in a
pane of its own. A bare "Find" would not lead anyone to expect that.

## `[menu: view]` View

- `[id: sidebar.toggle]` Sidebar ✓ — `Ctrl+Shift+B`
- `[id: pane.titles]` Pane Titles ✓
- `[id: view.fullScreen]` Full Screen ✓ — `F11`
- ⟨header: Font⟩
- `[id: font.increase]` Larger — `Ctrl+=`, `Ctrl++`
- `[id: font.decrease]` Smaller — `Ctrl+-`
- `[id: font.reset]` Reset — `Ctrl+0`
- ⟨header: Scrollback⟩
- `[id: view.scrollUp]` Page Up — `Shift+PageUp`
- `[id: view.scrollDown]` Page Down — `Shift+PageDown`

## `[menu: pane]` Pane

- ⟨header: Split⟩
- `[id: pane.splitRight]` Right — `Ctrl+Shift+D`
- `[id: pane.splitDown]` Down — `Ctrl+Shift+E`
- `[id: pane.popOut]` Pop Out — `Ctrl+Shift+U`
- ⟨header: Go To⟩
- `[id: pane.nextInSidebar]` Next — `Ctrl+PageDown`
- `[id: pane.previousInSidebar]` Previous — `Ctrl+PageUp`
- `[id: pane.next]` Last Used — `Ctrl+Tab`
- `[id: pane.previous]` Last Used, Reversed — `Ctrl+Shift+Tab`
- `[id: view.switcher]` All Panes… — `Ctrl+Shift+A`
- `[id: sidebar.focus]` Sidebar — `Ctrl+Shift+L`

"Pop Out" because the pane leaves the split and lands on the stage,
which is somewhere rather than nowhere.

## `[menu: machine]` Machine

- ⟨header: Open Here⟩
- `[id: conn.terminal]` Terminal — `Ctrl+Shift+T`
- `[id: conn.command]` Command…
- `[id: conn.files]` Files
- `[id: conn.tunnel]` Tunnel…
- `[id: conn.socks]` SOCKS Proxy…
- ⟨header: Files⟩
- `[id: files.goTo]` Go to Directory… — `Ctrl+Shift+G`
- `[id: files.copies]` Remembered Copies…
- ⟨header: This Machine⟩
- `[id: conn.log]` Connection Log
- `[id: shell.setup]` Shell Setup ✓

The first group is the same list in the same order as the plus on a
machine's row in the sidebar.

## `[menu: servers]` Servers

- ⟨header: Connect To⟩ *(only when there is a saved server)*
- ⟨built at run time⟩ the saved servers, under their bare names
- ⟨separator⟩
- `[id: server.connect]` Connect… — `Ctrl+Shift+N`
- ⟨separator⟩
- `[id: server.add]` Add Server…
- `[id: server.editThis]` Edit This Server…
- `[id: server.forget]` Forget This Server…
- ⟨header: SSH Keys⟩
- `[id: sshkey.make]` New Key…
- `[id: sshkey.lock]` Lock Keys

"Keys" means SSH keys here and nowhere else. The keyboard kind is
"shortcuts" throughout.

## `[menu: share]` Share

- ⟨header: Windows⟩
- `[id: serve.window]` Serve This One…
- `[id: serve.attach]` Attach to Another…
- ⟨header: Agent⟩
- `[id: agent.hand]` Share This Pane with an Agent… *(reads "Add This
  Pane to the Share…" once a share is open)*
- `[id: agent.take]` Remove Pane
- `[id: agent.share]` Show Share… *(only while a share is open)*
- `[id: agent.typed]` Typing History…

## `[menu: options]` Options

- `[id: view.theme]` Theme…
- ⟨header: Starter Files⟩
- `[id: view.themesStart]` New Theme File…
- `[id: shortcuts.write]` New Shortcuts File
- ⟨header: Reload⟩
- `[id: view.themesReload]` Themes
- `[id: shortcuts.reload]` Shortcuts
- `[id: server.reload]` Server List
- ⟨separator⟩
- `[id: help.files]` File Locations

## `[menu: help]` Help

- `[id: palette.open]` All Commands… — `Ctrl+Shift+K`
- `[id: help.shortcuts]` Shortcuts and Commands
- ⟨separator⟩
- `[id: view.log]` Window Log
- ⟨separator⟩
- `[id: app.about]` About gridterm

---

# On no menu

- `[id: menu.open]` Show the menu bar — `F10`. A menu row that opens
  the menu bar would be circular, and the bar can be hidden.
- `[id: shell.default]` New terminal on the default shell. Not the
  same as New Terminal: it opens on the machine's default **and
  forgets the shell you picked**, permanently. It is a reset, and it
  is on the plus menu where the shells are.

---

# The ids

The ids changed with the menus, while nothing outside this repo has a
shortcuts file naming them. What the user reads and what the code
calls it now agree:

| was | is | why |
|---|---|---|
| `keys.start` | `shortcuts.write` | a key is an SSH key here |
| `keys.reload` | `shortcuts.reload` | the same |
| `help.keys` | `help.shortcuts` | the same, and the dialog is retitled |
| `keys.lock` | `sshkey.lock` | this one really is an SSH key |
| `key.make` | `sshkey.make` | and it agrees with the line above it |
| `serve.takeOver` | `serve.attach` | the term is gone from the window |
| `conn.close` | `sidebar.closeRow` | it closes a row, not a connection |
| `panel.toggle` | `sidebar.toggle` | the user has always read sidebar |
| `panel.focus` | `sidebar.focus` | the same |
| `pane.unsplit` | `pane.popOut` | the line says Pop Out |

# Still to decide

- **Nine menus is a wide bar.** Nothing has been dropped, and two were
  added. Worth watching on a narrow window.
- **Mnemonics are not drawn.** `&File &Edit &View &Pane &Machine
  &Servers Sh&are &Options &Help` has no clashes if they are wanted.
- **The rows that only make sense sometimes are not greyed out.** Edit
  This Server and Forget This Server are live whatever the focused
  pane is on.
