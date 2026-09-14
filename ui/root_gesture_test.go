package ui

import (
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
)

// opener is a widget that puts a dialog up on a press, the way a menu
// bar title does.
type opener struct {
	root   *Root
	dialog Widget
}

func (*opener) Layout(Size)    {}
func (*opener) Draw(grid.View) {}
func (o *opener) HandleMouse(ev input.MouseEvent) (bool, error) {
	if ev.Kind == input.MousePress && !ev.Button.IsWheel() {
		o.root.PushModal(o.dialog)
	}
	return true, nil
}

// TestRootPressThatOpensADialogHandsItTheGesture checks press, drag,
// release on a menu. The press opens the menu and the drag has to reach
// it, or the standard way to use one highlights nothing.
func TestRootPressThatOpensADialogHandsItTheGesture(t *testing.T) {
	r := &Root{}
	dialog := &mouser{}
	tree := &opener{root: r, dialog: dialog}
	r.SetWidget(tree)
	r.Layout(Rect{Cols: 10, Rows: 4})

	r.HandleMouse(pressAt(1, 0))
	r.HandleMouse(moveTo(3, 2))
	r.HandleMouse(releaseAt(3, 2))

	var moved bool
	for _, ev := range dialog.seen {
		if ev.Kind == input.MouseMove && ev.Col == 3 && ev.Row == 2 {
			moved = true
		}
	}
	if !moved {
		t.Errorf("the dialog saw %v, want the drag that followed the press", dialog.kinds())
	}
}

// TestRootPressThatOpensADialogKeepsTheTreeOutOfIt checks the other
// half: the widget that was pressed does not go on receiving the
// gesture once a dialog is over it.
func TestRootPressThatOpensADialogKeepsTheTreeOutOfIt(t *testing.T) {
	r := &Root{}
	dialog := &mouser{}
	tree := &opener{root: r, dialog: dialog}
	r.SetWidget(tree)
	r.Layout(Rect{Cols: 10, Rows: 4})

	r.HandleMouse(pressAt(1, 0))
	before := len(dialog.seen)
	r.HandleMouse(moveTo(3, 2))

	if len(dialog.seen) == before {
		t.Fatal("the drag reached nobody")
	}
	// And the pointer is let go when the button comes up, so the next
	// press starts afresh rather than being swallowed.
	r.HandleMouse(releaseAt(3, 2))
	if r.held.Held() {
		t.Error("the pointer is still held after the button came up")
	}
}
