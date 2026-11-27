package source

import "testing"

func TestPosition(t *testing.T) {
	fs := NewFileSet()
	a := fs.AddFile("a.rr", []byte("entity Δέλτα\n  id uuid\n"))
	b := fs.AddFile("b.rr", []byte("role admin"))

	tests := []struct {
		file   *File
		offset int
		want   string
	}{
		{a, 0, "a.rr:1:1"},
		{a, 7, "a.rr:1:8"},
		{a, 17, "a.rr:1:13"}, // "Δέλτα" is 5 runes, 10 bytes
		{a, 18, "a.rr:2:1"},
		{a, 20, "a.rr:2:3"},
		{a, a.Size(), "a.rr:3:1"},
		{b, 0, "b.rr:1:1"},
		{b, 5, "b.rr:1:6"},
		{b, b.Size(), "b.rr:1:11"},
	}
	for _, tt := range tests {
		p := tt.file.Pos(tt.offset)
		if got := fs.Position(p).String(); got != tt.want {
			t.Errorf("Position(%s+%d) = %s, want %s", tt.file.Name(), tt.offset, got, tt.want)
		}
		if got := fs.File(p); got != tt.file {
			t.Errorf("File(%s+%d) = %v, want %s", tt.file.Name(), tt.offset, got, tt.file.Name())
		}
	}
	if got := fs.Position(NoPos); got.IsValid() {
		t.Errorf("Position(NoPos) = %v, want invalid", got)
	}
}

func TestLineText(t *testing.T) {
	fs := NewFileSet()
	f := fs.AddFile("x.rr", []byte("one\r\ntwo\n\nfour"))
	want := []string{"one", "two", "", "four"}
	if f.LineCount() != len(want) {
		t.Fatalf("LineCount = %d, want %d", f.LineCount(), len(want))
	}
	for i, w := range want {
		if got := f.LineText(i + 1); got != w {
			t.Errorf("LineText(%d) = %q, want %q", i+1, got, w)
		}
	}
	if got := f.LineText(0); got != "" {
		t.Errorf("LineText(0) = %q, want empty", got)
	}
	if got := f.Line(f.LineStart(4)); got != 4 {
		t.Errorf("Line(LineStart(4)) = %d, want 4", got)
	}
}
