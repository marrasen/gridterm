# How kakel words things

Every dialog, button, menu row, command title and error in kakel is
written to these rules. They are here so a new one comes out right the
first time rather than being rewritten later.

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
   not why kakel has to offer it.
5. **Buttons are one verb**, from a small fixed set: `OK` `Add` `Cancel`
   `Close` `Retry` `Wait` `Save` `Create` `Delete` `Remove` `Replace`
   `Skip` `Open` `Run` `Connect` `Generate` `Show` `Type`. No pronouns:
   never `Make it`, `Close it`, `Keep them`, `Leave it`. And never a
   button that renames itself to say what the next press does: that is
   rule 9. Use a tick box.
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
   the missing word.
3. **`…` means the command asks something before it acts.** A command
   that shows something and asks nothing has none: `About kakel`,
   `File Locations`, `Typing History`.
4. **Every word a title loses goes into `commandAlso`** in
   `view/keys.go`, along with the
   ordinary synonyms and both spellings, so whoever learned the old
   wording still finds the command by typing it.

A toggle is named for the thing it shows and the tick says the rest.
"Show or hide" and "on or off" never appear in a title.

**Command IDs never change.** `edit.copy`, `pane.close` and the rest are
what a saved key binding, a menu line and the shortcuts file refer to.
Renaming one breaks whatever pointed at it. Titles are free to edit. If
an ID ever has to move, `keys.Renamed` keeps the old one working.

## Status text: a row on the sidebar

A row says what a connection **is**, now. It is scanned in a narrow
column beside a dozen others rather than read, so it is written to be
taken in at a glance and not to be read as a sentence.

1. **A state is a short phrase, lower case, no full stop:** `connecting`,
   `taken over`, `connection lost`, `cancelled`. A sentence in a column
   is the thing read last, if at all.
2. **The row is already named, so the state does not name it again.** The
   row carries the machine's or window's name beside the state. `the
   window stopped sharing` says it twice; `stopped sharing` says it once.
3. **The label is what the row is. The note is one fact about it** — who
   it is for, where it came from, how fast it is going. Not a second
   sentence.
4. **The same thing gets the same words on every surface.** A connection
   that dropped reads `connection lost` whether it carried a pane or a
   window. Where a state is shown in two places it is written once.
5. **A note is shown while it is changing and then goes quiet.** It
   stands for four seconds (`noteFor` in `view/view.go`), and then comes
   off the row; the pointer on the row brings it back, and so does the
   keyboard. A note takes its room from
   the name, which is what the row is for, so it earns that room while
   it is saying something new and gives it back afterwards. A copy says
   `3 of 7` and then `4 of 7`, so its note is up the whole time it runs.
6. **Nothing lives only in a note.** A note can be dropped by a narrow
   sidebar and is gone four seconds after it settles, so anything that
   has to be read later belongs where it can be read later. The reason a
   connection was lost goes in the account, not on the row.

A row also carries what can be done with it, drawn as something to
press: `Watch the traffic`, `Close the tunnel`. Those are actions, not
states, and follow the button rules rather than these.

## Error headings

One shape: `Couldn't <verb> <object>`. A partial success is `<What
worked>, but <what did not>`. Two failures the user cannot act on
differently get one heading between them — the body carries the error.
A command that fails is headed by `failedTitle` in `app/app.go`:
`Couldn't open the tunnel`.

## Reach for a widget, not a sentence

These exist so a dialog does not need a paragraph explaining itself. Use
them before adding body text. They are gunim's widgets.

| Instead of | Use |
|---|---|
| A sentence saying a field is ignored here | `Disabled` on the field: drawn dim, and it takes no keys |
| A field whose answer must be one of a list, typed and then refused | `widget.Dropdown` |
| A dialog that only says something worked | a toast: `(*app).worked` |

## A word shown in two places is written once

The tick boxes an agent is told to look for live in `agent`
(`agent.BoxRestart` and the rest), because the MCP server quotes them
back. The window's other words are written where the view makes them.

**Copies drift.** The MCP server once told an agent to ask the user to
tick "Restart a closed connection" after the dialog had renamed that box.
Both halves worked; only the pair was wrong.

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
anything. Do not delete it.

The general form, which is not only about wording: **a test asserting
that two things agree is usually a sign they should be one thing.** The
test is a runtime check of an invariant that could be structural, and it
only checks the pairings some test happens to reach. Making the two into
one moves the check to the compiler, where it covers every use and costs
no lines. The same applies to a default written in two places, a limit, a
path, a format.

One test from this codebase was worse than useless: it asserted that a
dialog carried a sentence explaining that a field would be ignored. Rule
9 says that sentence is a bug. The test pinned the bug in place, so
fixing the field failed the build. Watch for tests that defend the things
these rules remove.

## Dialog titles

A dialog opened by a command is titled with the command's title, less
the `…`: `Serve This Window`, `Agent Permissions`. Where it names its
target it may drop the verb: `Run Command on <where>`.
