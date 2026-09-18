# Fonts that come with gridterm

## PxPlus IBM VGA8

`PxPlus_IBM_VGA8.ttf` is the IBM VGA 8x16 character set as a TrueType
font, from the Ultimate Oldschool PC Font Pack by VileR.

- Home: <https://int10h.org/oldschool-pc-fonts/>
- Licence: Creative Commons Attribution-ShareAlike 4.0 International,
  in `LICENSE-PxPlus-IBM-VGA8.txt`.
- Copy taken from cool-retro-term, which carries the same licence file:
  <https://github.com/Swordfish90/cool-retro-term/tree/master/app/qml/fonts/oldschool-pc-fonts>

It holds 782 glyphs: the IBM code page 437 set, the box-drawing and
block characters, Greek, Cyrillic, arrows and a good part of Latin
Extended. It has nothing beyond that, so a rune it cannot draw falls
back to a system font the way any other typeface does.

Share-alike covers the font file. It is data the program reads, not part
of the program, so gridterm's own licence is unaffected. A copy of
gridterm that ships this file has to ship the licence beside it, which
is what this directory is for.

## Go Mono

The other faces compiled in are Go Mono, from `golang.org/x/image`,
under the Go project's BSD licence. They come in through the module
rather than as files here.
