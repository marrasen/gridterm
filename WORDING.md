# How gridterm words things

Every dialog, button, menu row, command title and error in gridterm is
written to these rules. They are here so a new one comes out right the
first time rather than being rewritten later.

[DIALOGS.md](DIALOGS.md) and [COMMANDS.md](COMMANDS.md) are what the
rules were applied to: one entry per dialog and one row per command, with
the wording as it should read. When you add a dialog or a command, add it
there too.

## The rules

1. **Say what it is. Never what it is not.** No "not a path", "rather
   than", "nothing else", "nothing changes", "not what the shell ran".
2. **Use the words software uses.** Lost, invalid, failed, not found,
   cancel, retry, create, remove, save, reload. A program does not "go",
   "give up", "let go", "hold", "forget", "take", "kick out" or "say
   something else".
3. **The title is the message.** The body adds only what the title
   cannot: which machine, which path, one consequence. If the title says
   it all, there is no body.
4. **Never explain the program's reasoning.** The user needs the choice,
   not why gridterm has to offer it.
5. **Buttons are one verb**, from a small fixed set: `OK` `Cancel`
   `Close` `Retry` `Wait` `Save` `Create` `Delete` `Remove` `Replace`
   `Skip` `Open` `Run` `Connect`. No pronouns: never `Make it`, `Close
   it`, `Keep them`, `Leave it`.
6. **`Cancel` cancels what the dialog is about. `Close` dismisses a
   dialog and leaves things running.** The same meaning everywhere.
7. **Errors are two or three words, sentence case, no full stop:**
   `Invalid passphrase`, `Enter a command`.
8. **Placeholders are a format or the word `Optional`.** Never a
   sentence, never "what to call it".
9. **If a sentence exists to excuse a behaviour, change the behaviour.**
   Retry limits, fields that are silently ignored and buttons that rename
   themselves are bugs wearing an explanation.
10. **A warning earns its place only where the action exposes something
    or cannot be undone**, and then it is one sentence stating the
    consequence.

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

"Keys" means SSH keys throughout, and the keyboard kind is "shortcuts".

## Command titles

A command title is read in a flat list in the palette, with no menu or
header around it, and it heads the notice shown when the command fails.

1. **A title is a name, in Title Case, two to four words, verb first:**
   `Close Pane`, `Reload Themes`, `Open Tunnel…`. No articles, no "this
   pane's", no commas, no clauses.
2. **A title stands alone.** `Split Right`, never bare `Right`. A menu
   row may be shorter than the title only where its header already says
   the missing word; those rows are listed at the end of
   [COMMANDS.md](COMMANDS.md).
3. **`…` means the command asks something before it acts.** A command
   that shows something and asks nothing has none: `About gridterm`,
   `File Locations`, `Typing History`.
4. **Every word a title loses goes into `AlsoFind`**, along with the
   ordinary synonyms and both spellings, so whoever learned the old
   wording still finds the command by typing it.

A toggle is named for the thing it shows and the tick says the rest.
"Show or hide" and "on or off" never appear in a title.

**Command IDs never change.** `edit.copy`, `pane.close` and the rest are
what a saved key binding, a menu line and the shortcuts file refer to.
Renaming one breaks whatever pointed at it. Titles are free to edit.

**A command title is also an error heading.** `(*app).reporting` shows
`<Title> failed`, with the ellipsis stripped: `Open Tunnel failed`. Every
title has to read correctly in that frame.

## Error headings

One shape: `Could not <verb> <object>`. A partial success is `<What
worked>, but <what did not>`. Two failures the user cannot act on
differently get one heading between them — the body carries the error.

## Reach for a widget, not a sentence

These exist so a dialog does not need a paragraph explaining itself. Use
them before adding body text.

| Instead of | Use |
|---|---|
| A paragraph explaining one field | `ui.Field.Hint` — one line, drawn while that field has focus |
| A sentence saying a field is ignored here | `ui.Field.Disabled` — drawn dim, takes no keys, focus steps over it |
| "Ctrl+down and Ctrl+up step through…" | Nothing: a field with `Options` draws `Ctrl+↑↓` beside it |
| A dialog that only says something worked | `(*app).say(text)` — one line along the bottom row |
| `Copy` on a notice with nothing to copy | `ui.Notice.SetNoCopy()` |

A dialog is as wide as its longest line and stops there, so a body
written as one long line is a body with its end cut off. Wrap by hand, or
through `wrapLines(…, errorLineWidth)`.

## Every word is a constant, said once

**No display string is written twice, and none is written as a literal
where it is used.** Button titles, field labels and dialog titles live in
`wording.go`; the tick boxes an agent is told to look for live in
`agent`, because the MCP server quotes them back; a command's title and
the menu rows that offer it are one constant.

A menu row that says exactly its command's title carries no `Title` at
all — `ui.MenuItem` falls back to the registered one. The row cannot
drift from the command because there is nothing to drift.

Two reasons, and the second costs the most:

**Copies drift.** The MCP server once told an agent to ask the user to
tick "Restart a closed connection" after the dialog had renamed that box.
Both halves worked; only the pair was wrong.

**A test has to find a button before it can press one.** Given a literal,
the only handle is the wording, so every test that presses Cancel is
coupled to the word "Cancel" without caring what it says. Reword and a
hundred tests break that were never about wording. Given a constant, the
handle is the constant, and rewording costs nothing.

Rewording this whole window once cost about 700 lines of test churn. With
the constants in place it costs none.

## Tests

The rules above are about what the program says. This one is about what
is written to check it.

**A test that restates the code tests nothing.** If a dialog declares two
buttons and a test asserts that it has those two buttons in that order,
the test holds no information the code does not. It cannot fail except
when somebody deliberately changes the declaration, and then it fails by
construction rather than because anything broke. The author of the list
writes the assertion about the list; both say the same thing twice.

Worth testing is what is *computed or chosen*, where reading the code
does not tell you the answer:

| Restates the code | Worth testing |
|---|---|
| These are the buttons, in this order | A stray Enter lands on the safe one |
| The body says this sentence | A field that cannot apply takes no keys |
| The menu has this row | The row names a command that is registered |
| The title is this string | Every line fits the width the dialog draws |
| This value was set | A wrong passphrase is asked about again |

**If you find a test that restates the code, do not correct it.** Correcting
it is the work this rule exists to avoid. Instead:

1. **Make it unnecessary.** Usually the test exists because the same
   wording is in two places and something has to check they agree. Put
   the wording in one place and the test has nothing left to catch.
2. **Delete it** once it is unnecessary, or if it always was.

A test that is merely *coupled* to wording — one that presses a button by
its title on the way to checking something real — is not restating
anything. Do not delete it. Give it a constant to hold instead.

One test from this codebase was worse than useless: it asserted that a
dialog carried a sentence explaining that a field would be ignored. Rule
9 says that sentence is a bug. The test pinned the bug in place, so
fixing the field failed the build. Watch for tests that defend the things
these rules remove.

## Where the shared titles are listed

Some titles are one constant shared by a command and the dialog it opens
— `helpTitle`, `copiesTitle`, `typedTitle` and the rest.
[COMMANDS.md](COMMANDS.md) lists them.
