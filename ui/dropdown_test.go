package ui

import (
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
)

// servers is a drop-down whose keys are not what it shows, the way a
// jump host is saved as an id and shown as a name.
func servers() *Field {
	f := NewField()
	f.Choices = []Choice{
		{Key: "", Label: "None"},
		{Key: "id-backup", Label: "backup"},
		{Key: "id-bastion", Label: "bastion"},
		{Key: "id-web", Label: "web"},
	}
	f.SetFocus(true)
	return f
}

func typed(r rune) input.Event {
	return input.Event{Kind: input.Text, Rune: r, NormalText: true}
}

// A drop-down opens with space, is walked with the arrows and picks with
// Enter, and what it holds is the key of what was picked.
func TestADropDownPicksWithTheArrowsAndEnter(t *testing.T) {
	f := servers()
	picked := ""
	f.OnPick = func(key string) { picked = key }

	if took, _ := f.HandleKey(press(input.KeySpace, 0)); !took || !f.IsOpen() {
		t.Fatal("space did not open the list")
	}
	for range 2 {
		f.HandleKey(press(input.KeyDown, 0))
	}
	if took, _ := f.HandleKey(press(input.KeyEnter, 0)); !took {
		t.Fatal("Enter went past an open list")
	}
	if f.IsOpen() {
		t.Error("the list is still open after a pick")
	}
	if f.Text() != "id-bastion" || f.Label() != "bastion" {
		t.Errorf("it holds %q shown as %q, want id-bastion shown as bastion", f.Text(), f.Label())
	}
	if picked != "id-bastion" {
		t.Errorf("OnPick was told %q", picked)
	}
}

// Escape puts an open list away and changes nothing, and the key goes no
// further: the dialog behind it stays open.
func TestEscapePutsTheListAwayAndChangesNothing(t *testing.T) {
	f := servers()
	f.SetText("id-web")
	f.HandleKey(press(input.KeySpace, 0))
	f.HandleKey(press(input.KeyUp, 0))
	if took, _ := f.HandleKey(press(input.KeyEscape, 0)); !took {
		t.Error("Escape went past an open list, and would close the dialog")
	}
	if f.IsOpen() || f.Text() != "id-web" {
		t.Errorf("open %v holding %q, want closed and still id-web", f.IsOpen(), f.Text())
	}
}

// Closed, a drop-down hands on the keys that move between fields and
// press the dialog's button, the way any field does.
func TestAClosedDropDownHandsOnTheDialogsKeys(t *testing.T) {
	f := servers()
	for _, key := range []input.Key{input.KeyUp, input.KeyDown, input.KeyEnter, input.KeyTab, input.KeyEscape} {
		if took, _ := f.HandleKey(press(key, 0)); took {
			t.Errorf("%v was taken by a closed drop-down", key)
		}
	}
}

// Ctrl+Up and Ctrl+Down step through the answers without opening the
// list, which is what a field with options has always taken.
func TestCtrlArrowsStepThroughADropDown(t *testing.T) {
	f := servers()
	f.HandleKey(press(input.KeyDown, input.ModCtrl))
	if f.IsOpen() {
		t.Error("stepping opened the list")
	}
	if f.Text() != "id-backup" {
		t.Errorf("it holds %q after one step from None, want id-backup", f.Text())
	}
	f.HandleKey(press(input.KeyUp, input.ModCtrl))
	if f.Text() != "" {
		t.Errorf("it holds %q after stepping back, want None", f.Text())
	}
}

// Typing a name picks the answer it starts, and nothing is typed into
// the field: what it holds is always one of its keys.
func TestTypingANamePicksItInADropDown(t *testing.T) {
	f := servers()
	for _, r := range "bas" {
		if took, _ := f.HandleKey(typed(r)); !took {
			t.Fatalf("%q went past the drop-down", r)
		}
	}
	if f.Text() != "id-bastion" {
		t.Errorf("typing bas chose %q, want id-bastion", f.Text())
	}
	// A letter nothing starts with after what went before starts again.
	f.HandleKey(typed('w'))
	if f.Text() != "id-web" {
		t.Errorf("typing w chose %q, want id-web", f.Text())
	}
	f.HandleKey(typed('z'))
	if f.Text() != "id-web" {
		t.Errorf("a letter nothing starts with changed it to %q", f.Text())
	}
}

// A space part way through a name is part of it, and a space with
// nothing typed opens the list.
func TestASpaceInANameIsTypedNotPressed(t *testing.T) {
	f := NewField()
	f.Choices = ChoicesOf("Keep both", "Keep neither", "Skip")
	f.SetFocus(true)
	f.HandleKey(press(input.KeyDown, 0)) // any key that is not typing
	for _, r := range "Keep n" {
		f.HandleKey(typed(r))
	}
	if f.IsOpen() {
		t.Error("the space in the name opened the list")
	}
	if f.Text() != "Keep neither" {
		t.Errorf("typing Keep n chose %q", f.Text())
	}
	f.HandleKey(press(input.KeyEnd, 0))
	f.HandleKey(typed(' '))
	if !f.IsOpen() {
		t.Error("a space with nothing typed did not open the list")
	}
}

// A disabled drop-down does not open and takes no keys.
func TestADisabledDropDownDoesNotOpen(t *testing.T) {
	f := servers()
	f.Disabled = true
	if took, _ := f.HandleKey(press(input.KeySpace, 0)); took || f.IsOpen() {
		t.Error("a disabled drop-down opened")
	}
}

// dropForm is a form with a drop-down between two text fields, with the
// focus on the drop-down.
func dropForm(t *testing.T) (*Form, *Field, *int) {
	t.Helper()
	closed := 0
	f := NewForm("Add Server", func() { closed++ })
	f.Style = formStyled()
	f.AddField("Name", nil)
	via := f.AddField("Jump host", servers())
	via.SetFocus(false)
	f.AddField("Folders", nil)
	f.AddButton(Button{Title: "Save", Do: func() error { return nil }})
	f.Layout(Size{Cols: 60, Rows: 24})
	f.SetFocus(true)
	keyTo(t, f, press(input.KeyDown, 0))
	if at, _ := f.Focused(); at != 1 {
		t.Fatalf("the focus is on row %d, want the drop-down", at)
	}
	return f, via, &closed
}

// The open list is drawn under the field, over the rows below it, with
// every answer on it.
func TestAnOpenListIsDrawnUnderTheField(t *testing.T) {
	f, via, _ := dropForm(t)
	keyTo(t, f, press(input.KeySpace, 0))
	g := drawForm(f, 60, 24)
	box := f.Box()
	y := box.Y + f.rowsTop() + 1
	// A row for the frame, then the answers.
	for i, want := range []string{"None", "backup", "bastion", "web"} {
		row := rowOf(g, y+2+i)
		at := box.X + f.fieldX()
		if got := strings.TrimSpace(row[at : at+len(want)]); got != want {
			t.Errorf("row %d of the list reads %q, want %q", i, row, want)
		}
	}
	if !via.IsOpen() {
		t.Error("drawing closed the list")
	}
	// Closed, the rows below are the form's again.
	keyTo(t, f, press(input.KeyEscape, 0))
	g = drawForm(f, 60, 24)
	if strings.Contains(gridText(g), "bastion") {
		t.Error("the list is still drawn after it was put away")
	}
}

// Clicking the field opens the list, clicking an answer picks it, and a
// click anywhere else puts the list away without closing the dialog.
func TestAListIsOpenedAndPickedWithTheMouse(t *testing.T) {
	f, via, closed := dropForm(t)
	drawForm(f, 60, 24)
	box := f.Box()
	x, y := box.X+f.fieldX(), box.Y+f.rowsTop()+1
	click := func(col, row int) {
		t.Helper()
		mouseTo(t, f, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row})
		drawForm(f, 60, 24)
	}

	click(x, y)
	if !via.IsOpen() {
		t.Fatal("clicking the drop-down did not open it")
	}
	click(x+1, y+2+2) // bastion, the third answer, under the frame
	if via.IsOpen() || via.Text() != "id-bastion" {
		t.Errorf("open %v holding %q after clicking bastion", via.IsOpen(), via.Text())
	}

	click(x, y)
	click(0, 0) // outside the dialog
	if via.IsOpen() {
		t.Error("a click elsewhere left the list open")
	}
	if *closed != 0 {
		t.Error("a click that put the list away closed the dialog too")
	}
	if via.Text() != "id-bastion" {
		t.Errorf("a click elsewhere changed it to %q", via.Text())
	}
}

// Moving the focus off a drop-down puts its list away.
func TestLeavingADropDownPutsItsListAway(t *testing.T) {
	f, via, _ := dropForm(t)
	keyTo(t, f, press(input.KeySpace, 0))
	keyTo(t, f, press(input.KeyTab, 0))
	if via.IsOpen() {
		t.Error("the list stayed open after Tab")
	}
	if at, _ := f.Focused(); at != 2 {
		t.Errorf("Tab left the focus on %d, want the next field", at)
	}
}

// A drop-down turned off while its list is open loses the focus, and the
// list goes with it.
func TestADropDownTurnedOffPutsItsListAway(t *testing.T) {
	f, via, _ := dropForm(t)
	keyTo(t, f, press(input.KeySpace, 0))
	via.Disabled = true
	f.EnsureFocusable()
	if via.IsOpen() {
		t.Error("the list of a field that lost the focus is still open")
	}
	if _, _, open := f.dropRect(); open {
		t.Error("the form still has a list to draw")
	}
}
