package edit

import "testing"

func TestEditUndoFind(t *testing.T) {
	b := New("hello\nworld")
	b.MoveTo(Pos{0, 5}, false)
	for _, r := range " big" {
		b.Insert(string(r))
	}
	b.Newline()
	if got := b.Text(); got != "hello big\n\nworld" {
		t.Fatal(got)
	}
	b.Undo() // the newline
	b.Undo() // " big" typed in one go
	if got := b.Text(); got != "hello\nworld" {
		t.Fatal(got)
	}
	b.Redo()
	b.Redo()
	if got := b.Text(); got != "hello big\n\nworld" {
		t.Fatal(got)
	}
	b.MoveTo(Pos{2, 0}, false)
	b.Backspace() // joins with the empty line
	b.Backspace() // joins with "hello big"
	if got := b.Text(); got != "hello bigworld" {
		t.Fatal(got)
	}
	b.SelectAll()
	b.Insert("a\tb")
	b.Newline()
	if got := b.Text(); got != "a\tb\n" || b.Cur != (Pos{1, 0}) {
		t.Fatalf("%q %v", got, b.Cur)
	}
	b.SetText("one Two\nthree two")
	if s, e, ok := b.Find("two", Pos{0, 5}, true); !ok || s != (Pos{1, 6}) || e != (Pos{1, 9}) {
		t.Fatal(s, e, ok)
	}
	if s, _, ok := b.Find("two", Pos{1, 6}, false); !ok || s != (Pos{0, 4}) {
		t.Fatal(s, ok)
	}
	b.SelectWord(Pos{1, 1})
	if b.SelectedText() != "three" {
		t.Fatal(b.SelectedText())
	}
}

func TestLineOps(t *testing.T) {
	b := New("a\n\tb\nc")
	b.MoveTo(Pos{1, 1}, false)
	b.MoveLines(-1)
	if b.Text() != "\tb\na\nc" || b.Cur != (Pos{0, 1}) {
		t.Fatalf("%q %v", b.Text(), b.Cur)
	}
	b.Undo()
	if b.Text() != "a\n\tb\nc" {
		t.Fatalf("%q", b.Text())
	}
	b.MoveTo(Pos{0, 0}, false)
	b.MoveTo(Pos{2, 0}, true) // selects rows 0-1; row 2 isn't included
	b.ToggleComment("//")
	if b.Text() != "// a\n// \tb\nc" {
		t.Fatalf("%q", b.Text())
	}
	b.ToggleComment("//")
	if b.Text() != "a\n\tb\nc" {
		t.Fatalf("%q", b.Text())
	}
	b.Indent("\t", false)
	if b.Text() != "\ta\n\t\tb\nc" {
		t.Fatalf("%q", b.Text())
	}
	b.Undo()
	b.MoveTo(Pos{2, 0}, false)
	b.DuplicateLines(true)
	b.DeleteLines()
	b.DeleteLines()
	if b.Text() != "a\n\tb" || b.Cur.Row != 1 {
		t.Fatalf("%q %v", b.Text(), b.Cur)
	}
	if n := b.ReplaceAll("B", "x"); n != 1 || b.Text() != "a\n\tx" {
		t.Fatal(n, b.Text())
	}
}
