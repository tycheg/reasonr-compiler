package diag

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tycheg/reasonr-compiler/internal/source"
)

const hintText = "hint"

// WriteText prints diagnostics for a person:
//
//	taxi/customer.rr:44:5: error E0902: message
//	   44 |     orderStarted(orderId: orderId, contact: phone)
//	      |                                             ^^^^^
//	   = note
//	   = hint: fix
func WriteText(w io.Writer, fset *source.FileSet, list []*Diagnostic) error {
	var b strings.Builder
	for _, d := range list {
		pos := fset.Position(d.Range.Start)
		fmt.Fprintf(&b, "%s: %s %s: %s\n", pos, d.Severity(), d.Code(), d.Message())
		gutter := 5
		if f := fset.File(d.Range.Start); f != nil {
			gutter = writeSnippet(&b, f, d.Range)
		}
		pad := strings.Repeat(" ", gutter-2)
		for _, n := range d.Notes {
			fmt.Fprintf(&b, "%s= %s", pad, n.Msg)
			if n.Range.IsValid() && n.Range != d.Range {
				fmt.Fprintf(&b, " (%s)", fset.Position(n.Range.Start))
			}
			b.WriteByte('\n')
		}
		for _, fx := range d.Fixes {
			fmt.Fprintf(&b, "%s= %s: %s\n", pad, hintText, fx.Msg)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeSnippet prints a source line with the range underlined and returns
// the width of the line number field.
func writeSnippet(b *strings.Builder, f *source.File, r source.Range) int {
	start := f.Position(r.Start)
	line := f.LineText(start.Line)
	num := strconv.Itoa(start.Line)
	gutter := len(num) + 3
	if gutter < 5 {
		gutter = 5
	}

	// A tab is shown as two spaces, as the lexer counts it.
	display := strings.ReplaceAll(line, "\t", "  ")
	fmt.Fprintf(b, "%*s | %s\n", gutter, num, display)

	endCol := utf8.RuneCountInString(line) + 1
	if r.End.IsValid() && f.Contains(r.End) {
		if end := f.Position(r.End); end.Line == start.Line {
			endCol = end.Column
		}
	}
	var marker strings.Builder
	col := 1
	for _, c := range line {
		if col >= start.Column {
			break
		}
		if c == '\t' {
			marker.WriteString("  ")
		} else {
			marker.WriteByte(' ')
		}
		col++
	}
	n := endCol - start.Column
	if n < 1 {
		n = 1
	}
	marker.WriteString(strings.Repeat("^", n))
	fmt.Fprintf(b, "%*s | %s\n", gutter, "", marker.String())
	return gutter
}

// JSONVersion is the version of the JSON output schema. It changes on incompatible changes.
const JSONVersion = 1

type jsonOutput struct {
	Version     int              `json:"version"`
	Diagnostics []jsonDiagnostic `json:"diagnostics"`
}

type jsonDiagnostic struct {
	Code     Code       `json:"code"`
	Severity string     `json:"severity"`
	File     string     `json:"file"`
	Range    *jsonRange `json:"range,omitempty"`
	Message  string     `json:"message"`
	Notes    []jsonNote `json:"notes,omitempty"`
	Fixes    []jsonFix  `json:"fixes,omitempty"`
}

type jsonNote struct {
	Message string     `json:"message"`
	File    string     `json:"file,omitempty"`
	Range   *jsonRange `json:"range,omitempty"`
}

type jsonFix struct {
	Message string     `json:"message"`
	Edits   []jsonEdit `json:"edits,omitempty"`
}

type jsonEdit struct {
	File  string     `json:"file"`
	Range *jsonRange `json:"range"`
	Text  string     `json:"text"`
}

type jsonRange struct {
	Start jsonPos `json:"start"`
	End   jsonPos `json:"end"`
}

type jsonPos struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func toJSONRange(fset *source.FileSet, r source.Range) (string, *jsonRange) {
	if !r.IsValid() {
		return "", nil
	}
	s := fset.Position(r.Start)
	e := s
	if r.End.IsValid() {
		e = fset.Position(r.End)
	}
	return s.Filename, &jsonRange{
		Start: jsonPos{Line: s.Line, Column: s.Column},
		End:   jsonPos{Line: e.Line, Column: e.Column},
	}
}

// WriteJSON prints diagnostics in machine-readable form (for agents and CI).
// Lines and columns start at 1; columns count runes.
func WriteJSON(w io.Writer, fset *source.FileSet, list []*Diagnostic) error {
	out := jsonOutput{Version: JSONVersion, Diagnostics: []jsonDiagnostic{}}
	for _, d := range list {
		file, rng := toJSONRange(fset, d.Range)
		jd := jsonDiagnostic{
			Code:     d.Code(),
			Severity: d.Severity().String(),
			File:     file,
			Range:    rng,
			Message:  d.Message(),
		}
		for _, n := range d.Notes {
			nf, nr := toJSONRange(fset, n.Range)
			jd.Notes = append(jd.Notes, jsonNote{Message: n.Msg.String(), File: nf, Range: nr})
		}
		for _, fx := range d.Fixes {
			jf := jsonFix{Message: fx.Msg.String()}
			for _, e := range fx.Edits {
				ef, er := toJSONRange(fset, e.Range)
				jf.Edits = append(jf.Edits, jsonEdit{File: ef, Range: er, Text: e.NewText})
			}
			jd.Fixes = append(jd.Fixes, jf)
		}
		out.Diagnostics = append(out.Diagnostics, jd)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}
