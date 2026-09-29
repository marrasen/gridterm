package look

import (
	"image/color"
	"testing"

	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/themes"
)

// A theme that writes its frame down draws like a text screen's boxes:
// the default button's words in activeFG, a black shadow under each
// button, a double rule when asked for, and the row in front in
// currentFG. One that does not keeps gunim's own.
func TestAThemesFrameReachesTheButtonsAndTheSidebar(t *testing.T) {
	base := themes.Built()[0]
	base.Frame = nil
	plain, err := Of(base)
	if err != nil {
		t.Fatal(err)
	}
	live := theme.NewLive(plain.Theme)
	if widget.ButtonShadow.Get(live).A != 0 || widget.DialogBorderLines.Get(live) != 1 {
		t.Fatal("a theme with no frame casts a shadow or doubles the rule")
	}
	base.Frame = &themes.Frame{FG: "#aaaaaa", BG: "#0000aa", Border: "double", ActiveFG: "#ffff55", CurrentFG: "#55ffff", ButtonShadow: "#000000"}
	boxed, err := Of(base)
	if err != nil {
		t.Fatal(err)
	}
	live = theme.NewLive(boxed.Theme)
	if got := widget.ButtonPrimaryInk.Get(live); got != (color.NRGBA{R: 0xff, G: 0xff, B: 0x55, A: 0xff}) {
		t.Fatalf("the default button's words are %v", got)
	}
	if widget.ButtonShadow.Get(live) != (color.NRGBA{A: 0xff}) || widget.DialogBorderLines.Get(live) != 2 {
		t.Fatal("a boxed theme casts no shadow, or keeps a single rule")
	}
	base.Frame.ButtonShadow = ""
	unshadowed, err := Of(base)
	if err != nil {
		t.Fatal(err)
	}
	if widget.ButtonShadow.Get(theme.NewLive(unshadowed.Theme)).A != 0 {
		t.Fatal("a frame that names no shadow casts one")
	}
	if got := RowActiveInk.Get(live); got != (color.NRGBA{R: 0x55, G: 0xff, B: 0xff, A: 0xff}) {
		t.Fatalf("the row in front is written in %v", got)
	}
}

// A theme's shape scales gunim's corners and room from their defaults,
// and its motion sets how things move; a value out of range is refused.
func TestAThemesShapeAndMotionReachGunim(t *testing.T) {
	base := themes.Built()[0]
	two, half, text := 2.0, 0.5, 16.0
	base.Shape = &themes.Shape{Corners: &two, Room: &half, Text: &text}
	base.Motion = themes.MotionStill
	th, err := Of(base)
	if err != nil {
		t.Fatal(err)
	}
	live := theme.NewLive(th.Theme)
	if got := widget.ButtonRadius.Get(live); got != 2*widget.ButtonRadius.Default() {
		t.Errorf("corners at 2, a button's are %v", got)
	}
	if got := widget.ButtonPadding.Get(live); got != 0.5*widget.ButtonPadding.Default() {
		t.Errorf("room at 0.5, a button's padding is %v", got)
	}
	if got := SidebarRow.Get(live); got != 0.5*SidebarRow.Default() {
		t.Errorf("room at 0.5, a sidebar row is %v tall", got)
	}
	if widget.TextSize.Get(live) != 16 {
		t.Errorf("the text is %v, want 16", widget.TextSize.Get(live))
	}
	if widget.Settle.Get(live).Response >= widget.Settle.Default().Response {
		t.Error("still, things settle no faster than calm")
	}
	for _, bad := range []themes.Theme{
		{Shape: &themes.Shape{Room: &text}},
		{Motion: "bouncy"},
	} {
		b := base
		b.Shape, b.Motion = bad.Shape, bad.Motion
		if _, err := Of(b); err == nil {
			t.Errorf("the shape %+v and motion %q were taken", bad.Shape, bad.Motion)
		}
	}
}
