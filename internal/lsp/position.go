package lsp

import (
	"bytes"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/tycheg/reasonr-compiler/internal/source"
)

// pathFromURI converts a file:// URI into a file path; "" when not a file.
func pathFromURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	// file:///C:/x → C:/x
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.Clean(filepath.FromSlash(p))
}

// uriFromPath converts a file path into a file:// URI.
func uriFromPath(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// utf16Len returns the length of a text in UTF-16 code units.
func utf16Len(b []byte) int {
	n := 0
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		b = b[size:]
		n += runeLen16(r)
	}
	return n
}

func runeLen16(r rune) int {
	if w := utf16.RuneLen(r); w > 0 {
		return w
	}
	return 1
}

// utf16Offset returns the byte offset in line that corresponds to character
// ch in UTF-16 code units (no further than the end of the line).
func utf16Offset(line []byte, ch int) int {
	off, n := 0, 0
	for off < len(line) && n < ch {
		r, size := utf8.DecodeRune(line[off:])
		if r == '\n' {
			break
		}
		n += runeLen16(r)
		off += size
	}
	return off
}

// lspPos converts a file position into an LSP position.
func lspPos(f *source.File, p source.Pos) Position {
	line := f.Line(p)
	start := f.Offset(f.LineStart(line))
	off := f.Offset(p)
	return Position{Line: line - 1, Character: utf16Len(f.Source()[start:off])}
}

// lspRange converts a file range into an LSP range.
func lspRange(f *source.File, r source.Range) Range {
	end := r.End
	if !end.IsValid() || !f.Contains(end) {
		end = r.Start
	}
	return Range{Start: lspPos(f, r.Start), End: lspPos(f, end)}
}

// filePos converts an LSP position into a file position; positions past the
// end of a line or the file are clamped to the end.
func filePos(f *source.File, p Position) source.Pos {
	if p.Line < 0 {
		return f.Pos(0)
	}
	if p.Line >= f.LineCount() {
		return f.Pos(f.Size())
	}
	start := f.Offset(f.LineStart(p.Line + 1))
	return f.Pos(start + utf16Offset(f.Source()[start:], p.Character))
}

// textOffset converts an LSP position into an offset in the text.
func textOffset(text []byte, p Position) int {
	off := 0
	for line := 0; line < p.Line; line++ {
		i := bytes.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	return off + utf16Offset(text[off:], p.Character)
}

// textPos converts an offset in the text into an LSP position.
func textPos(text []byte, off int) Position {
	line, start := 0, 0
	for i := 0; i < off; i++ {
		if text[i] == '\n' {
			line++
			start = i + 1
		}
	}
	return Position{Line: line, Character: utf16Len(text[start:off])}
}

// applyChange applies a document change to the text.
func applyChange(text []byte, c contentChange) []byte {
	if c.Range == nil {
		return []byte(c.Text)
	}
	start := textOffset(text, c.Range.Start)
	end := textOffset(text, c.Range.End)
	if end < start {
		start, end = end, start
	}
	out := make([]byte, 0, len(text)-(end-start)+len(c.Text))
	out = append(out, text[:start]...)
	out = append(out, c.Text...)
	return append(out, text[end:]...)
}
