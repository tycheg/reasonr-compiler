package lsp

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzServer: an arbitrary document text and position do not crash server
// requests (a panic in a handler would become an internal error).
func FuzzServer(f *testing.F) {
	text, err := os.ReadFile(filepath.Join("testdata", "shop", "order.rr"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(string(text), 500)
	f.Add(strings.Replace(string(text), "reason.trim()", "reason.", 1), 640)
	f.Add("aggregate A\n  command B(x int)\n    b(\n", 30)
	f.Add("value V\n  x [", 15)
	f.Add("test \"t\"\n  when\n    [x].", 25)
	dir := f.TempDir()
	path := filepath.Join(dir, "m.rr")
	f.Fuzz(func(t *testing.T, text string, off int) {
		s := &server{conn: newConn(strings.NewReader(""), io.Discard), opts: Options{Delay: -1, Log: io.Discard},
			ws: newWorkspace(), initialized: true}
		uri := uriFromPath(path)
		call := func(method string, params any) {
			b, _ := json.Marshal(params)
			if _, rerr := s.handle(&message{ID: json.RawMessage("1"), Method: method, Params: b}); rerr != nil && rerr.Code == codeInternalError {
				t.Fatalf("%s: %v", method, rerr)
			}
		}
		call("textDocument/didOpen", didOpenParams{TextDocument: textDocumentItem{URI: uri, Version: 1, Text: text}})
		if off < 0 {
			off = -off
		}
		pos := textPos([]byte(text), off%(len(text)+1))
		p := textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: uri}, Position: pos}
		call("textDocument/hover", p)
		call("textDocument/definition", p)
		call("textDocument/completion", p)
		call("textDocument/references", referenceParams{textDocumentPositionParams: p})
		call("textDocument/semanticTokens/full", documentParams{TextDocument: p.TextDocument})
		call("textDocument/formatting", documentParams{TextDocument: p.TextDocument})
		call("textDocument/codeAction", codeActionParams{TextDocument: p.TextDocument, Range: Range{Start: pos, End: pos}})
	})
}
