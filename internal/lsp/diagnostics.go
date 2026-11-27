package lsp

import (
	"bytes"
	"encoding/json"
	"path/filepath"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/format"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

var hintText = "hint"

// lspDiagnostic converts a compiler diagnostic into an LSP diagnostic.
// Notes with a place become related information, hints without edits are
// appended to the message; fixes with edits become quick fixes (codeAction).
func (s *server) lspDiagnostic(snap *snapshot, f *source.File, d *diag.Diagnostic) Diagnostic {
	out := Diagnostic{
		Range:    lspRange(f, d.Range),
		Severity: severityError,
		Code:     string(d.Code()),
		Source:   "reasonr",
		Message:  d.Message(),
	}
	if d.Severity() == diag.Warning {
		out.Severity = severityWarning
	}
	fset := snap.mod.FileSet
	for _, n := range d.Notes {
		nf := fset.File(n.Range.Start)
		if nf == nil {
			out.Message += "\n" + n.Msg.String()
			continue
		}
		out.RelatedInformation = append(out.RelatedInformation, relatedInformation{
			Location: Location{URI: uriFromPath(filepath.FromSlash(nf.Name())), Range: lspRange(nf, n.Range)},
			Message:  n.Msg.String(),
		})
	}
	for _, fx := range d.Fixes {
		if len(fx.Edits) == 0 {
			out.Message += "\n" + hintText + ": " + fx.Msg.String()
		}
	}
	return out
}

// codeAction offers fixes of the diagnostics that touch the range.
func (s *server) codeAction(params json.RawMessage) (any, *rpcError) {
	var p codeActionParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	snap, ref := s.file(p.TextDocument.URI)
	if ref == nil {
		return []codeAction{}, nil
	}
	src := ref.file.Src
	from, to := filePos(src, p.Range.Start), filePos(src, p.Range.End)
	fset := snap.mod.FileSet
	actions := []codeAction{}
	for _, d := range snap.diagnostics()[ref.path] {
		if d.Range.End < from || d.Range.Start > to {
			continue
		}
		var fixes []diag.Fix
		for _, fx := range d.Fixes {
			if len(fx.Edits) > 0 {
				fixes = append(fixes, fx)
			}
		}
		for _, fx := range fixes {
			changes := map[string][]TextEdit{}
			ok := true
			for _, e := range fx.Edits {
				ef := fset.File(e.Range.Start)
				if ef == nil {
					ok = false
					break
				}
				uri := uriFromPath(filepath.FromSlash(ef.Name()))
				changes[uri] = append(changes[uri], TextEdit{Range: lspRange(ef, e.Range), NewText: e.NewText})
			}
			if !ok {
				continue
			}
			actions = append(actions, codeAction{
				Title:       fx.Msg.String(),
				Kind:        "quickfix",
				Diagnostics: []Diagnostic{s.lspDiagnostic(snap, src, d)},
				IsPreferred: len(fixes) == 1,
				Edit:        workspaceEdit{Changes: changes},
			})
		}
	}
	return actions, nil
}

// formatting formats a document. A document with syntax errors is not
// formatted: there are no changes.
func (s *server) formatting(params json.RawMessage) (any, *rpcError) {
	var p documentParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	path := pathFromURI(p.TextDocument.URI)
	text, ok := s.text(path)
	if !ok {
		return []TextEdit{}, nil
	}
	out, diags := format.Source(source.NewFileSet(), path, text)
	if diag.HasErrors(diags) || bytes.Equal(out, text) {
		return []TextEdit{}, nil
	}
	return []TextEdit{minimalEdit(text, out)}, nil
}

// text returns the text of a file: an open document or from disk.
func (s *server) text(path string) ([]byte, bool) {
	if d := s.ws.docs[path]; d != nil {
		return d.text, true
	}
	if snap := s.ws.module(path).snap; snap != nil {
		if f := snap.files[path]; f != nil {
			return f.Src.Source(), true
		}
	}
	return nil, false
}

// minimalEdit returns one edit that turns old into new: only the lines
// between the common prefix and the common suffix are replaced.
func minimalEdit(old, new []byte) TextEdit {
	// The common prefix, by whole lines.
	pre := 0
	for i := 0; i < len(old) && i < len(new) && old[i] == new[i]; i++ {
		if old[i] == '\n' {
			pre = i + 1
		}
	}
	// The common suffix, by whole lines, not entering the common prefix.
	suf := 0
	for i := 1; i <= len(old)-pre && i <= len(new)-pre && old[len(old)-i] == new[len(new)-i]; i++ {
		if old[len(old)-i] == '\n' {
			suf = i - 1
		}
	}
	return TextEdit{
		Range:   Range{Start: textPos(old, pre), End: textPos(old, len(old)-suf)},
		NewText: string(new[pre : len(new)-suf]),
	}
}
