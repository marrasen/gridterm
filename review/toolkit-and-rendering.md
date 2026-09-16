# The toolkit and the renderer

Packages `ui`, `ui/files`, `ui/term`, `render`, `grid`, `glyph`, plus
the main-package consumers (`panel.go`, `sidebar.go`, `region.go`,
`modals.go`, `pad.go`, `attach.go`). Every non-test file read. `go vet`
is clean.

## Summary

The contracts in `ui/widget.go` are unusually precise, and the hard
parts -- mouse capture, the cursor-claim rule, wide-character repair,
the compositor's idle path, the screen-sharing ordering argument -- are
right and explained. Damage tracking holds: no cell is written with a
genuinely changing value every frame.

The problems are of two kinds. A few live bugs, two of them in work
done in the last day (the key bar's "Go to" and the held terminal size).
And a large amount of duplication -- three list state machines, four
strip painters, five truncation helpers, two colour mixers, two chord
vocabularies -- with the toolkit's own rules (modifier handling, who
sizes a widget) applied differently in each copy. That is the same
disease as the main package, in a milder form.

## Findings, most serious first

### 1. "^G Go to" on the file browser's key bar is dead

`ui/files/keybar.go:31`, `ui/files/browser.go:388-407` (`wired`),
`ui/files/browser.go:552-579` (`press`). **Verified.**

The bar advertises `^G Go to`, but `Browser.wired` has no case for it,
so the cell is drawn on `OffBG` as if nothing were behind it. Clicking
it calls `press`, whose Ctrl branch knows only C, X, V and D, and falls
through to `return false, nil`. The key works when *typed*, because
`Pane.HandleKey` handles Ctrl+G one layer below the bar. So the bar and
the keyboard disagree about a key the bar is the only advertisement for.

The test that came with the feature checked the bar listed the key and
that the pane took the keypress. It never clicked the bar cell. Fix:
one case in each of `wired` and `press` -- or better, move the Ctrl+G
handling out of `Pane` into `Browser`, so the bar's table is the single
source again, which is what `press`'s own comment claims it is.

Closed by 0320a9c Give dialogs one key rule, tell the user why a connection went.

### 2. Escape cancels the clipboard before the type-to-find

`ui/files/browser.go:583` against `ui/files/pane.go:700`.

`Browser.HandleKey` runs `press` before handing the key to the pane,
and `press` claims Escape whenever the clipboard is non-empty. Type
three letters to jump to a name while something is on the clipboard,
press Escape to abandon the jump: the clipboard is silently emptied and
the find buffer is left standing. Escape should unwind the innermost
thing first.

Closed by 0320a9c Give dialogs one key rule, tell the user why a connection went.

### 3. A held terminal size has no read side, and the host is never told

`ui/term/term.go:227` (`Layout` ignores `size` while held), `:288`
(cursor), `:378` (mouse), `panel.go:224`.

While held, `Draw` copies the emulator top-left into the box. If the
held size is larger than the box, the cursor lands outside the view and
is hidden, and the columns and rows past the box are neither drawn nor
clickable. If smaller, mouse coordinates past the emulator are used
unclamped as selection anchors.

And the machine the pane runs on is never told. `refreshPanel` writes
only "watched by 1". The `farNote` machinery that says *"at 120x40"* is
used only on the pane that is *watching*, never on the pane whose size
was taken. `Terminal.Box()` and `Terminal.Held()` have **no callers
outside tests** -- the feature's read side was never wired. `Terminal.
Dirty()` is dead too.

Naming hazard: `Terminal.Box() ui.Size` collides with the toolkit's
`Boxed.Box() Rect` contract. Different signature, so no confusion for
the compiler; a reader has to check.

Partly closed by 4bc67cc Give the taken-over windows a type with its invariant written down: the host's row says the size somebody else set.
`Layout` still ignores the size while a pane is held, the cursor and the
mouse are still clipped to the box, and `Terminal.Dirty()` still has no
caller. That is Step 5 of `review/todo-plan.md`.

### 4. Every widget handles modifiers differently, and `Form` not at all

`ui/form.go:233-271` against `ui/list.go:336`, `ui/menu.go:283`,
`ui/files/pane.go:694`, `ui/files/browser.go:557`, `ui/field.go:134`.

Five policies in one toolkit. `List` and `Menu` hand any chord out.
`files.Pane` does the same, spelled differently. `Browser.press` takes
Ctrl only. `Field` rejects Alt and Super, then acts on Ctrl. **`Form`
checks nothing**: Ctrl+Tab, Alt+Down, Ctrl+Escape and Shift+Enter all
move its focus or press a button and are swallowed. So Ctrl+Tab (bound
to "next pane") does nothing while any dialog is open, with no error
saying why. `Form` should mirror `Menu`: decline any modifier it did
not ask for.

Closed by 0320a9c Give dialogs one key rule, tell the user why a connection went.

### 5. `Chooser` swallows every key, so the accelerators die while one is up

`ui/chooser.go:242`.

`Menu`, `Palette` and `Form` return `false` for keys they do not want,
and `Root.HandleKey` then offers them to the accelerators -- which is
what keeps close and quit alive over a dialog. `Chooser` returns `true`
unconditionally. Open a chooser and F10, Ctrl+K, the font-size keys and
every split and tab chord stop working. The comment's argument ("a key
reaching a pane behind it would be typed into something the user cannot
see") applies equally to the other three, which solved it by declining
rather than swallowing.

Closed by 0320a9c Give dialogs one key rule, tell the user why a connection went.

### 6. Ctrl+K is an accelerator, so the shell never sees kill-line

`app.go:732`.

`{Key: KeyK, Mods: ModCtrl}: "palette.open"` runs before the widget
tree. Ctrl+K is readline's kill-to-end-of-line and is in a lot of
people's fingers; here it opens the palette and the shell gets nothing.
Every other accelerator is Ctrl+Shift, a function key, or a chord no
terminal claims; the key bar even documents avoiding F10 for this exact
reason. Ctrl+Shift+K or Ctrl+Shift+P would match the rest of the table.

Closed by 0320a9c Give dialogs one key rule, tell the user why a connection went.

### 7. `Field` resizes itself inside `Draw`, and two widgets depend on it

`ui/field.go:202-203`.

`Draw` sets `f.cols` from the view and calls `scroll()`, which mutates
where the text is scrolled to. That breaks the contract at
`ui/widget.go:74` -- and it is load-bearing. `Palette` never calls
`Layout` on its query field at all; the only thing that ever gives it a
width is `Draw`. `Form.box()` widens the dialog when an error is set,
and neither `SetError` nor `press` re-lays the fields; they are right on
the next frame only because `Draw` re-derives the width. So
`Form.layoutFields` is nearly dead weight and the toolkit's Layout/Draw
split does not size the one widget with scroll state. Either make
`Field.Draw` read-only and have the callers call `Layout`, or say in
`Widget` that a widget may re-derive from the view. One or the other.

Closed by 0320a9c Give dialogs one key rule, tell the user why a connection went.

### 8. `files.Browser` measures from the view but routes clicks from the stored size

`ui/files/browser.go:361-378` (`Draw` uses `v.Size()`) against `:340`,
`:510`, `:636-654` (all `b.size`).

Whenever a parent hands the browser a view smaller than the size it
promised -- which `ui/widget.go:79-82` explicitly allows -- the panes
are drawn in one set of columns and clicked in another. No parent does
that today, so it is latent. `sidebar.go:29-34` solves the identical
problem the other way and documents why. `ChildArea`'s contract says
the area must be the one `Layout` used, so `b.size` is the right answer
and `Draw` is the side to fix.

### 9. `glyph.Atlas` drops GPU pages without deallocating them

`glyph/atlas.go:168`, `:185`.

`SetSize` and `SetFonts` do `*a = *next`, replacing `a.pages`
(`[]*ebiten.Image`) with no `Deallocate()` on the old ones -- unlike
`render.Layer.ensure` and `Compositor.Remove`, which are careful about
it. Each page is a 1024×1024 texture. Holding Ctrl+= through the font
range allocates dozens that only the finalizer reclaims.

Closed by b00f97e Make the font fallback one decision, reported once.

## Damage tracking

The discipline is good and holds. What writes a changing value does so
on a bounded clock, deliberately: `pulse` steps on 200 ms and every row
steps together; `meter.Rate` recomputes at most once per window;
`jobNote` changes only on a file count; `sidebar.drawRow` and
`files.drawKeys` write every cell once with the same value. **No cell is
written with a genuinely changing value every frame.**

Two hazards worth naming:

- **Multi-write-per-frame is handled by `ui/buffer.go`, but only for
  the six widgets that use it.** `grid.Set` dirties a row on any write
  that differs from the current value, so fill-then-overwrite dirties a
  row even when the final value is unchanged. `List.paint` fills every
  row and then fills the selected row again; the buffer is what saves
  it. Anything new drawn straight onto a layer must be write-once. That
  rule should be in `ui/widget.go`, not in each widget's comment.
- **Nested buffers.** `Chooser.paint` draws a `List` through the
  chooser's buffer, and `List.Draw` has one of its own: two full copies
  per frame for the same cells.

Per-frame allocation on the draw path, adjacent to damage: `Rate.Past()`
and `meter.Bars` allocate per row per frame via `panel.go:467`;
`Tabs.labels()` and `Menubar.labels()` allocate per call and are called
from both mouse routing and paint.

## Threading

`ui/term` is the only place `ui` meets another goroutine, and the
atomics are right. `title` is an `atomic.Pointer` polled by the drawing
goroutine, which is what the `Config` comment asks for. `Hold`/`Release`
are reached only through the pump or already on the drawing goroutine.
**No unsynchronised field is shared between the network goroutines and
the drawing goroutine.**

Three things to know rather than fix:

- `Terminal.Text()`, `Resync` and `Watch` take `t.mu` and render a whole
  fresh grid; `Draw` needs the same lock. A watcher resyncing, or an
  agent polling `Look` twenty times a second, can stall a frame. A
  latency coupling, not a race.
- `feed.Screen` (`attach.go:215`) does a blocking channel send while
  holding both `t.mu` and `watchMu`. Safe only because `drain()`
  immediately precedes it. The `Watcher` doc says neither method may
  block; this one relies on an argument two files away. A
  `select`/`default` after the drain would make it locally true.
- `Session.Resize` is called from the drawing goroutine while
  `writeLoop` and `readLoop` are in `Write` and `Read`. Safe for an ssh
  channel and a local pty; an unstated requirement on the interface.
  **Speculation** that every implementation honours it.

## Duplication

Every pair asked about exists.

- **Three scrolled-list state machines.** `List`, `Menu` and `Palette`
  each keep the same `at`/`top` invariant with their own move, scroll,
  clamp and reveal, and three different stopping rules. `Menu` and
  `Palette` could both be a `List` with a custom row painter, which is
  what `Chooser` already is.
- **Four ways to paint a one-row strip.** `Tabs.paintStrip`,
  `Menubar.paintBar`, `files.drawKeys`, `main.drawRow`. The first two go
  through `buffer`; the last two hand-roll the write-once discipline
  `buffer` exists to provide.
- **Two identical label-layout functions.** `Tabs.labels` and
  `Menubar.labels`: twelve lines, one pad constant apart.
- **Two identical "divide n cells across a width" helpers.**
  `Browser.paneCell` and `keyCell`, with the same rationale comment.
- **Two identical colour mixers, plus a third.** `ui.blend` and
  `main.mix` are byte-for-byte the same; `render.blend` takes a float.
- **Five truncation helpers, two sharing a name with opposite
  behaviour.** `ui.trimTo` cuts silently; `main.trimTo` appends an
  ellipsis. Plus `files.trimTail`, `files.trimLeft`, `files.trimTitle`,
  and open-coded cluster loops in `Menu.paintTitle` and
  `Palette.drawTitle`. User-visible: a long label in the sidebar list is
  cut with no sign, while a long machine name one row above gets "…".
- **Two chord vocabularies.** `ui.Chord` with a derived `String()`, and
  `files.fkey` with the display spelling hand-written (`"^C"`).

Closed by 1707651 Fold the duplicated pieces the review named into one of each.

## Size and cohesion

- **`grid/grid.go` (779)**: cell model, `Art`, damage, wide-character
  repair, cursor, selection, and the Unicode width helpers. Split out
  `art.go`, `selection.go`, `text.go`; ~470 lines of grid remain.
- **`ui/files/pane.go` (748)**: model, async read protocol,
  type-to-find, row formatting, three trims, and the widget. Split out
  `find.go` and `rows.go`.
- **`ui/form.go` (716)**: split out `formlayout.go`; move `trimTo` to a
  shared `ui/text.go` with the other truncation.
- **`ui/files/browser.go` (682)**: split out `clipboard.go`.
- **`ui/list.go` (617)**: split out `listrow.go`; `blend` moves to a
  shared colour helper and absorbs `main.mix`.
- **`ui/term/term.go` (616)**: split out `io.go` (the two byte-moving
  goroutines) and `input.go` (key and mouse encoding), leaving the
  widget and the hold state, which is where the thinking has to be
  visible.

`panel.go` and `render/renderer.go` are just under the line and each
has one job. Leave them.

## Smaller things

- `Split.Remove` returns "not a child" when the child's sibling is nil.
  Only reachable from `NewSplit(dir, a, nil)`, which nothing does.
- `Dock.Focused()` returns `d.rest`, which can be nil while `Children()`
  reports the panel, breaking the "must report one of them" contract.
  Only for a dock built with a nil rest.
- `drawShadow` paints two cells outside the rectangle `Box()` reports.
  Harmless for the frost, since the shadow is meant to fall on what is
  behind; a box that does not quite tell the truth.
- `Tabs.HandleMouse` and the browser swallow wheel notches over their
  own chrome; `Menu` and the sidebar deliberately let the wheel through.
  Scrolling stops over a tab strip or a pane divider for no stated
  reason.
- `region.place` asks `RowPads` before `Layout` and applies the answer
  after. For one frame after the list scrolls, a gap can sit on a row
  that is no longer a header. Self-correcting.

## What is sound

- **`ui/widget.go` itself.** The contracts are precise and each hard one
  names the failure it prevents. `Root.HandleMouse` and `deliverHeld`
  implement mouse capture exactly, including lost-release recovery and
  the press-opens-a-dialog case.
- **The cursor-claim rule.** The owner is hidden after the pass rather
  than before, which is the difference between a clean idle frame and a
  dirty one. No widget writes a cursor while unfocused.
- **`grid.Set` and `SetWide`.** `SetWide` checks both halves before
  clearing, precisely so an idle screen of CJK does not repaint -- a
  subtle bug someone hit and fixed properly.
- **`grid/view.go`.** Clipping, the stale-view contract, `SetWide`
  refusing to spill past the edge. No sharp edges.
- **The compositor's idle path.** `painted` and `full` as separate
  states; damage cleared only after every layer showing a grid has
  drawn; `lastScreen` catching ebiten's fresh offscreen; `lastAtlas`
  catching a re-rasterise the cell box would not reveal. Each is a bug
  that would show once in a while, each handled with a comment saying
  which.
- **`render/geometry.go`**: the padding model and `TakeCols`/`FitRows`
  are the right shape, and `region.go` uses them correctly.
- **`ui/buffer.go`**: thirty lines solving multi-write damage,
  see-through cells and cursor pass-through at once.
- **`ui/term/watch.go`**: the ordering argument for screen sharing --
  screen and stream under both locks, so a chunk cannot arrive before
  the screen that contains it -- is the hard part, and it is right,
  including the `behind`/`drain`/resync protocol.
- **`Commands`, `Keymap`, `Menu` and `Palette` as three doors onto one
  registry**, with `Root.run` refusing to consume a key for an
  unregistered command, so a stale binding cannot kill a key for good.
