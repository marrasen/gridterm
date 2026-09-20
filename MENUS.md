# The menus, to rearrange

Every menu and every line on it, in the order they are drawn today.

**Move lines, rename them, move them between menus, delete them, add
new ones. Keep the `id:` in square brackets at the front of each row
and I can follow an item wherever it ends up.** Rename the text after
the brackets as freely as you like; the id is what I match on.

A row you want that does not exist yet: write `[id: new]` and the
title you want, and I will pick the id and wire it up.

A row you want gone: delete it, or write `[id: …] DROP — <title>` if
you would rather see what you removed.

Menu titles have ids too, so a whole menu can be renamed or reordered.

`⟨separator⟩` is a rule between groups. Move and add those freely; no
id needed.

`⟨built at run time⟩` marks a line the window generates rather than
one written down in the code. Those cannot be reordered against each
other, only as a block — say where the block should sit.

---

## Menu bar order

The bar draws its menus left to right in this order. Reorder these
lines to reorder the bar.

1. `[menu: file]` **File**
2. `[menu: edit]` **Edit**
3. `[menu: view]` **View**
4. `[menu: connection]` **Connection**
5. `[menu: go]` **Go**
6. `[menu: servers]` **Servers**
7. `[menu: help]` **Help**

The Servers menu is added at run time and lands after the ones written
down, which is why it is second from the end.

---

## `[menu: file]` File

- `[id: pane.open]` New terminal
- ⟨separator⟩ *(only when there is more than one shell)*
- ⟨built at run time⟩ one line per shell this machine has: Command
  Prompt, PowerShell, Ubuntu (WSL) and so on
- ⟨separator⟩ *(only when there is more than one shell)*
- `[id: pane.splitRight]` Split right — `Ctrl+Shift+D`
- `[id: pane.splitDown]` Split down — `Ctrl+Shift+E`
- `[id: pane.unsplit]` Take this pane out of its split — `Ctrl+Shift+U`
- ⟨separator⟩
- `[id: keys.lock]` Forget unlocked keys and try the agent again
- ⟨separator⟩
- `[id: pane.close]` Close pane — `Ctrl+Shift+W`

## `[menu: edit]` Edit

- `[id: edit.copy]` Copy — `Ctrl+Shift+C`, `Ctrl+Insert`
- `[id: edit.paste]` Paste — `Ctrl+Shift+V`, `Shift+Insert`
- `[id: edit.pasteImage]` Paste the picture as a file… — `Ctrl+Alt+V`

## `[menu: view]` View

- `[id: panel.toggle]` Show or hide the connections — `Ctrl+Shift+B`
- `[id: panel.focus]` Go to the connections — `Ctrl+Shift+L`
- ⟨separator⟩
- `[id: font.increase]` Increase font size — `Ctrl+=`, `Ctrl++`
- `[id: font.decrease]` Decrease font size — `Ctrl+-`
- `[id: font.reset]` Reset font size — `Ctrl+0`
- ⟨separator⟩
- `[id: view.theme]` Colour theme…
- `[id: view.themesReload]` Reload colour themes
- `[id: view.themesStart]` Write a colour theme to edit…
- `[id: pane.titles]` Show or hide the line naming each pane
- ⟨separator⟩
- `[id: view.scrollUp]` Scroll back — `Shift+PageUp`
- `[id: view.scrollDown]` Scroll forward — `Shift+PageDown`
- `[id: pane.scrollback]` Search this pane's scrollback

## `[menu: connection]` Connection

- `[id: conn.terminal]` New terminal like this one — `Ctrl+Shift+T`
- `[id: conn.command]` Run a command…
- `[id: conn.tunnel]` Open a tunnel…
- `[id: conn.socks]` Open a SOCKS proxy…
- `[id: conn.files]` Browse files here
- `[id: files.copies]` Remembered copies…
- ⟨separator⟩
- `[id: conn.close]` Close this connection
- `[id: conn.clearFinished]` Clear finished connections

## `[menu: go]` Go

- `[id: pane.next]` Next pane, the one used before this — `Ctrl+Tab`
- `[id: pane.previous]` Previous pane, back the other way — `Ctrl+Shift+Tab`
- ⟨separator⟩
- `[id: view.switcher]` Show every pane… — `Ctrl+Shift+A`
- ⟨separator⟩
- `[id: pane.nextInSidebar]` Next pane, down the sidebar — `Ctrl+PageDown`
- `[id: pane.previousInSidebar]` Previous pane, up the sidebar — `Ctrl+PageUp`
- ⟨separator⟩
- `[id: palette.open]` Show all commands — `Ctrl+Shift+K`

## `[menu: servers]` Servers

- ⟨built at run time⟩ one **Connect to &lt;name&gt;** line per saved server
- ⟨separator⟩ *(only when there is at least one saved server)*
- `[id: server.connect]` Connect to a server — `Ctrl+Shift+N`
- `[id: server.add]` Add a server
- `[id: server.reload]` Reread the server list
- ⟨separator⟩
- `[id: serve.window]` Serve this window…
- `[id: serve.takeOver]` Connect to another window…
- ⟨separator⟩
- `[id: agent.hand]` Share this pane with an agent… *(reads "Add this
  pane to the share" once a share is open)*
- `[id: agent.take]` Take this pane out of the share
- `[id: agent.typed]` What the agent typed…
- `[id: agent.share]` Show the share… *(only while a share is open)*

## `[menu: help]` Help

- `[id: help.keys]` Keys and commands
- `[id: keys.start]` Write a starting keyboard shortcuts file
- `[id: keys.reload]` Reread the keyboard shortcuts
- `[id: help.files]` Where gridterm keeps its files
- ⟨separator⟩
- `[id: view.log]` Show what the window has logged

---

# On no menu

These are registered commands. They are in the palette and some have a
key, but no menu line. Move any of them into a menu above and I will
add the line.

- `[id: menu.open]` Show the menu bar — `F10`
- `[id: shell.default]` New terminal on the default shell
- `[id: shell.setup]` Shell setup on this machine, on or off
- `[id: key.make]` Make an SSH key…
- `[id: files.goTo]` Go to a directory… — `Ctrl+Shift+G`
- `[id: conn.disconnect]` Close the connection to this machine
- `[id: conn.log]` Show how this was reached
- `[id: server.editThis]` Edit this server…
- `[id: server.forget]` Forget this server…

---

# Not on the menu bar at all

For completeness, so you know what is already reachable elsewhere and
do not duplicate it.

**The plus on a machine's row in the sidebar** opens a menu of what can
be opened there: a terminal, a command, files, a tunnel, a SOCKS proxy,
the account of how it was reached, and a line per folder saved for that
machine. Those lines are built per machine and are not on the bar.

**The file browser's own keys** are on the bar along the bottom of its
pane: rename, copy, cut, paste, delete, make a directory. They are not
in the shortcuts file and not on a menu.

**The file viewer's own keys** likewise: Home, End, `Ctrl+R` reread,
`Ctrl+F` follow, `/` find, `Ctrl+H` hex, `:` go to line, `Ctrl+D`
close, `Ctrl+J` log view, `Ctrl+M` map.

**A pane's own question bar**, such as the choices a tunnel's pane
carries, or "Run again" on a pane whose program has ended.
