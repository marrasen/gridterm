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
	base.Frame = &themes.Frame{FG: "#aaaaaa", BG: "#0000aa", Border: "double", ActiveFG: "#ffff55", CurrentFG: "#55ffff"}
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
	if got := RowActiveInk.Get(live); got != (color.NRGBA{R: 0x55, G: 0xff, B: 0xff, A: 0xff}) {
		t.Fatalf("the row in front is written in %v", got)
	}
}
