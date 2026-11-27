// Package source holds the source files of a module and converts positions
// (byte offsets) to lines and columns.
package source

import (
	"fmt"
	"sort"
	"sync"
	"unicode/utf8"
)

// Pos is a position in a file set: the file's base offset plus a byte offset.
// The zero value NoPos means "no position".
type Pos int

// NoPos is the missing position.
const NoPos Pos = 0

// IsValid reports whether the position is set.
func (p Pos) IsValid() bool { return p != NoPos }

// Range is a half-open interval [Start, End) in one file.
type Range struct {
	Start, End Pos
}

// IsValid reports whether the range is set.
func (r Range) IsValid() bool { return r.Start.IsValid() }

// Position is a human-readable position. Lines and columns start at 1; the
// column counts runes (not bytes), as a person sees it in an editor.
type Position struct {
	Filename string
	Offset   int // byte offset from the start of the file
	Line     int
	Column   int
}

// IsValid reports whether the position is set.
func (p Position) IsValid() bool { return p.Line > 0 }

func (p Position) String() string {
	s := p.Filename
	if !p.IsValid() {
		if s == "" {
			return "-"
		}
		return s
	}
	if s != "" {
		s += ":"
	}
	return fmt.Sprintf("%s%d:%d", s, p.Line, p.Column)
}

// File is one source file.
type File struct {
	name  string
	base  int
	src   []byte
	lines []int // line start offsets; lines[0] == 0
}

// Name returns the name under which the file was added to the set.
func (f *File) Name() string { return f.name }

// Base returns the position of the file's first byte.
func (f *File) Base() int { return f.base }

// Size returns the file size in bytes.
func (f *File) Size() int { return len(f.src) }

// Source returns the file content. It must not be modified.
func (f *File) Source() []byte { return f.src }

// LineCount returns the number of lines in the file.
func (f *File) LineCount() int { return len(f.lines) }

// Pos converts a byte offset to a position. The offset may equal the file
// size: that is the end-of-file position.
func (f *File) Pos(offset int) Pos {
	if offset < 0 || offset > len(f.src) {
		panic(fmt.Sprintf("source: offset %d outside file %s (size %d)", offset, f.name, len(f.src)))
	}
	return Pos(f.base + offset)
}

// Offset converts a position to a byte offset.
func (f *File) Offset(p Pos) int {
	off := int(p) - f.base
	if off < 0 || off > len(f.src) {
		panic(fmt.Sprintf("source: position %d outside file %s", p, f.name))
	}
	return off
}

// Contains reports whether the position belongs to the file (end of file included).
func (f *File) Contains(p Pos) bool {
	return int(p) >= f.base && int(p) <= f.base+len(f.src)
}

// Line returns the line number (from 1) of a position.
func (f *File) Line(p Pos) int {
	return f.lineOf(f.Offset(p)) + 1
}

func (f *File) lineOf(off int) int {
	return sort.Search(len(f.lines), func(i int) bool { return f.lines[i] > off }) - 1
}

// Position converts a position to a line and a column.
func (f *File) Position(p Pos) Position {
	off := f.Offset(p)
	line := f.lineOf(off)
	start := f.lines[line]
	return Position{
		Filename: f.name,
		Offset:   off,
		Line:     line + 1,
		Column:   utf8.RuneCount(f.src[start:off]) + 1,
	}
}

// LineStart returns the position of the start of a line (lines start at 1).
func (f *File) LineStart(line int) Pos {
	if line < 1 || line > len(f.lines) {
		panic(fmt.Sprintf("source: line %d outside file %s", line, f.name))
	}
	return Pos(f.base + f.lines[line-1])
}

// LineText returns the text of a line without the line break (lines start at 1).
func (f *File) LineText(line int) string {
	if line < 1 || line > len(f.lines) {
		return ""
	}
	start := f.lines[line-1]
	end := len(f.src)
	if line < len(f.lines) {
		end = f.lines[line] - 1
	}
	if end > start && f.src[end-1] == '\r' {
		end--
	}
	return string(f.src[start:end])
}

// FileSet is a set of files with disjoint position ranges. Its methods are
// safe for concurrent use.
type FileSet struct {
	mu    sync.RWMutex
	base  int
	files []*File
}

// NewFileSet creates an empty set.
func NewFileSet() *FileSet {
	return &FileSet{base: 1}
}

// AddFile adds a file. The set keeps src and does not copy it.
func (s *FileSet) AddFile(name string, src []byte) *File {
	lines := []int{0}
	for i, b := range src {
		if b == '\n' {
			lines = append(lines, i+1)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := &File{name: name, base: s.base, src: src, lines: lines}
	// +1: the end-of-file position must not coincide with the start of the next file.
	s.base += len(src) + 1
	s.files = append(s.files, f)
	return f
}

// File returns the file a position belongs to, or nil.
func (s *FileSet) File(p Pos) *File {
	if !p.IsValid() {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := sort.Search(len(s.files), func(i int) bool { return s.files[i].base > int(p) }) - 1
	if i < 0 {
		return nil
	}
	if f := s.files[i]; f.Contains(p) {
		return f
	}
	return nil
}

// Position converts a position to a line and a column. For NoPos and
// positions of other sets it returns an empty Position.
func (s *FileSet) Position(p Pos) Position {
	if f := s.File(p); f != nil {
		return f.Position(p)
	}
	return Position{}
}

// Files returns the files in the order they were added.
func (s *FileSet) Files() []*File {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*File(nil), s.files...)
}
