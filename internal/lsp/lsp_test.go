package lsp

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/format"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

func TestDiagnostics(t *testing.T) {
	dir := copyModule(t, "shop")
	c := start(t)
	path := filepath.Join(dir, "order.rr")
	text := read(t, path)

	// An error in an open document: the text on disk does not matter.
	broken := strings.Replace(text, "cancelled(why: note)", "cancelled(why: 1)", 1)
	uri := c.open(path, broken)
	diags := c.waitDiags(uri)
	if got := codes(diags); !slices.Equal(got, []string{"E0202"}) {
		t.Fatalf("diagnostics %v, expected E0202", got)
	}
	d := diags[0]
	if d.Severity != severityError || d.Source != "reasonr" || !strings.Contains(d.Message, "decimal") && !strings.Contains(d.Message, "string") {
		t.Errorf("diagnostic %+v", d)
	}
	want := at(t, broken, "1)", 0)
	if d.Range.Start != want {
		t.Errorf("start %+v, expected %+v", d.Range.Start, want)
	}

	// A fix by a range edit.
	c.notify("textDocument/didChange", didChangeParams{
		TextDocument:   versionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []contentChange{{Range: &Range{Start: want, End: Position{Line: want.Line, Character: want.Character + 1}}, Text: "note"}},
	})
	if diags := c.waitDiags(uri); len(diags) != 0 {
		t.Fatalf("diagnostics left after the fix %v", codes(diags))
	}
}

func TestDiagnosticsExtension(t *testing.T) {
	dir := copyModule(t, "shop")
	c := start(t)
	// A document absent from disk belongs to the extension.
	path := filepath.Join(dir, "extensions", "acme", "extra.rr")
	// extend outside an extension is E1009; it is absent here, and the
	// reaction is checked against the base model.
	uri := c.open(path, "extend Order\n  on nope\n    return\n")
	diags := c.waitDiags(uri)
	if got := codes(diags); !slices.Equal(got, []string{"E1006"}) {
		t.Fatalf("diagnostics %v, expected E1006", got)
	}
}

func TestFormatting(t *testing.T) {
	dir := copyModule(t, "shop")
	c := start(t)
	path := filepath.Join(dir, "order.rr")
	text := read(t, path)
	messy := strings.Replace(text, "  status OrderStatus\n\n  index", "  status    OrderStatus\n\n\n\n  index", 1)
	uri := c.open(path, messy)
	c.waitDiags(uri)

	var edits []TextEdit
	if err := c.call("textDocument/formatting", documentParams{TextDocument: textDocumentIdentifier{URI: uri}}, &edits); err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 {
		t.Fatalf("edits %+v", edits)
	}
	got := string(applyChange([]byte(messy), contentChange{Range: &edits[0].Range, Text: edits[0].NewText}))
	want, _ := format.Source(source.NewFileSet(), path, []byte(messy))
	if got != string(want) {
		t.Errorf("after formatting:\n%s\nexpected:\n%s", got, want)
	}
	if edits[0].Range.Start.Line == 0 {
		t.Errorf("the edit touches the whole file: %+v", edits[0].Range)
	}

	// A formatted text does not change.
	c.notify("textDocument/didChange", didChangeParams{
		TextDocument:   versionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []contentChange{{Text: text}},
	})
	if err := c.call("textDocument/formatting", documentParams{TextDocument: textDocumentIdentifier{URI: uri}}, &edits); err != nil {
		t.Fatal(err)
	}
	if len(edits) != 0 {
		t.Errorf("edits for a formatted text: %+v", edits)
	}
}

func hoverAt(t *testing.T, c *client, uri string, pos Position) string {
	t.Helper()
	var h *hover
	if err := c.call("textDocument/hover", textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: uri}, Position: pos}, &h); err != nil {
		t.Fatal(err)
	}
	if h == nil {
		return ""
	}
	return h.Contents.Value
}

func TestHover(t *testing.T) {
	dir := copyModule(t, "shop")
	c := start(t)
	path := filepath.Join(dir, "order.rr")
	text := read(t, path)
	uri := c.open(path, text)
	c.waitDiags(uri)

	tests := []struct {
		needle string
		delta  int
		want   []string
	}{
		{"status == OrderStatus", 0, []string{"status OrderStatus", "field of `Order`"}},
		{"OrderStatus.cancelled\n      already", 13, []string{"OrderStatus.cancelled"}},
		{"Cancel(reason", 0, []string{"command(customer) Cancel(reason string)", "Cancel the order", "`Order`"}},
		{"alreadyCancelled()", 0, []string{"error alreadyCancelled"}},
		{"reason.trim", 0, []string{"reason string", "parameter of `Order.Cancel`"}},
		{"reason.trim", 7, []string{"trim", "built-in"}},
		{"note)", 0, []string{"let note string", "variable"}},
		{"why: note", 0, []string{"why string", "parameter of `Order.cancelled`"}},
		{"phone = phoneNumber", 0, []string{"phone string? personal", "Personal data of `Customer`"}},
		{"aggregate Order", 10, []string{"aggregate Order", "A customer order"}},
		{"placed\n", 0, []string{"OrderStatus.placed", "A new order"}},
		{"byCustomer", 0, []string{"index byCustomer(customer)"}},
		{"(admin)", 1, []string{"role admin"}},
		{"where customer", 6, []string{"customer Customer.Id?"}},
	}
	for _, tt := range tests {
		got := hoverAt(t, c, uri, at(t, text, tt.needle, tt.delta))
		for _, w := range tt.want {
			if !strings.Contains(got, w) {
				t.Errorf("%q: hover\n%s\ndoes not contain %q", tt.needle, got, w)
			}
		}
	}
	// The cursor right after a name gives hover for the name.
	if got := hoverAt(t, c, uri, at(t, text, "reason.trim", 6)); !strings.Contains(got, "reason string") {
		t.Errorf("hover after a name: %q", got)
	}
	if got := hoverAt(t, c, uri, at(t, text, "  ## Cancel", 0)); got != "" {
		t.Errorf("hover on an empty place: %q", got)
	}
}

func TestDefinitionReferences(t *testing.T) {
	dir := copyModule(t, "shop")
	c := start(t)
	path := filepath.Join(dir, "order.rr")
	text := read(t, path)
	uri := c.open(path, text)
	c.waitDiags(uri)

	var locs []Location
	if err := c.call("textDocument/definition", textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: uri}, Position: at(t, text, "alreadyCancelled()", 3),
	}, &locs); err != nil {
		t.Fatal(err)
	}
	if want := at(t, text, "alreadyCancelled\n", 0); len(locs) != 1 || locs[0].URI != uri || locs[0].Range.Start != want {
		t.Fatalf("definition %+v, expected %+v", locs, want)
	}

	// References to event cancelled: a call, the declaration and an extension reaction.
	refs := func(include bool) []Location {
		var p referenceParams
		p.TextDocument.URI = uri
		p.Position = at(t, text, "event cancelled", 6)
		p.Context.IncludeDeclaration = include
		var locs []Location
		if err := c.call("textDocument/references", p, &locs); err != nil {
			t.Fatal(err)
		}
		return locs
	}
	locs = refs(true)
	extURI := uriFromPath(filepath.Join(dir, "extensions", "acme", "order.rr"))
	var uris []string
	for _, l := range locs {
		uris = append(uris, l.URI)
	}
	if len(locs) != 3 || !slices.Contains(uris, extURI) {
		t.Fatalf("references %+v", locs)
	}
	if locs := refs(false); len(locs) != 2 {
		t.Fatalf("references without the declaration %+v", locs)
	}
}

func completeAt(t *testing.T, c *client, uri string, pos Position) []CompletionItem {
	t.Helper()
	var list completionList
	if err := c.call("textDocument/completion", textDocumentPositionParams{TextDocument: textDocumentIdentifier{URI: uri}, Position: pos}, &list); err != nil {
		t.Fatal(err)
	}
	return list.Items
}

func labels(items []CompletionItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func TestCompletion(t *testing.T) {
	dir := copyModule(t, "shop")
	c := start(t)
	path := filepath.Join(dir, "order.rr")
	text := read(t, path)

	tests := []struct {
		name     string
		replace  string // replace in the text
		with     string // with this; the cursor is at "|"
		want     []string
		dontWant []string
	}{
		{"string members", "let note = reason.trim()", "let note = reason.|", []string{"trim", "length", "startsWith"}, []string{"let"}},
		{"a started member name", "let note = reason.trim()", "let note = reason.tr|", []string{"trim"}, nil},
		{"argument names", "cancelled(why: note)", "cancelled(|)", []string{"why:"}, []string{"let"}},
		{"enum values", "if status == OrderStatus.cancelled", "if status == |", []string{"OrderStatus.paid", "OrderStatus.cancelled"}, nil},
		{"values after a dot", "if status == OrderStatus.cancelled", "if status == OrderStatus.|", []string{"placed", "paid"}, []string{"trim"}},
		{"entity commands", "    archived()\n", "    [id].|\n    archived()\n", []string{"Cancel", "Archive"}, []string{"cancelled"}},
		{"fields in where", "where customer == me", "where | == me", []string{"customer", "status", "id"}, nil},
		{"statement", "    return\n    let note", "    return\n    |\n    let note", []string{"let", "if", "reason", "status", "cancelled", "alreadyCancelled"}, []string{"entity", "and"}},
		{"variables", "cancelled(why: note)", "cancelled(why: n|)", []string{"note", "reason"}, nil},
		{"entity members", "  error alreadyCancelled\n", "  error alreadyCancelled\n  |\n", []string{"command", "event", "index"}, []string{"let"}},
		{"top level", "role admin\n", "role admin\n|\n", []string{"entity", "enum"}, []string{"let note"}},
		{"field type", "  status OrderStatus\n\n  index", "  status Ord|\n\n  index", []string{"OrderStatus", "OrderRow", "string"}, nil},
	}
	uri := uriFromPath(path)
	version := 1
	c.open(path, text)
	c.waitDiags(uri)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			with := strings.Replace(tt.with, "|", "", 1)
			edited := strings.Replace(text, tt.replace, tt.with, 1)
			if edited == text {
				t.Fatalf("no %q in the text", tt.replace)
			}
			pos := at(t, edited, "|", 0)
			edited = strings.Replace(text, tt.replace, with, 1)
			version++
			c.notify("textDocument/didChange", didChangeParams{
				TextDocument:   versionedTextDocumentIdentifier{URI: uri, Version: version},
				ContentChanges: []contentChange{{Text: edited}},
			})
			got := labels(completeAt(t, c, uri, pos))
			for _, w := range tt.want {
				if !slices.Contains(got, w) {
					t.Errorf("no %q among %v", w, got)
				}
			}
			for _, w := range tt.dontWant {
				if slices.Contains(got, w) {
					t.Errorf("extra %q among %v", w, got)
				}
			}
		})
	}
}

func TestCompletionSearchIndex(t *testing.T) {
	dir := copyModule(t, "shop")
	c := start(t)
	path := filepath.Join(dir, "order.rr")
	text := strings.Replace(read(t, path), "where customer == me", "where  == me", 1)
	uri := c.open(path, text)
	c.waitDiags(uri)
	items := completeAt(t, c, uri, at(t, text, " == me", 0))
	for _, it := range items {
		if it.Label == "customer" {
			if !strings.Contains(it.Detail, "index byCustomer") || !strings.HasPrefix(it.SortText, "0") {
				t.Errorf("index field: %+v", it)
			}
			return
		}
	}
	t.Fatalf("no field customer among %v", labels(items))
}

func TestSemanticTokens(t *testing.T) {
	dir := copyModule(t, "shop")
	c := start(t)
	path := filepath.Join(dir, "order.rr")
	text := read(t, path)
	uri := c.open(path, text)
	c.waitDiags(uri)
	var toks semanticTokens
	if err := c.call("textDocument/semanticTokens/full", documentParams{TextDocument: textDocumentIdentifier{URI: uri}}, &toks); err != nil {
		t.Fatal(err)
	}
	// Decode: position → kind and modifiers.
	type tok struct {
		typ  string
		mods []string
	}
	got := map[Position]tok{}
	var line, ch int
	for i := 0; i+5 <= len(toks.Data); i += 5 {
		d := toks.Data[i : i+5]
		if d[0] > 0 {
			line, ch = line+int(d[0]), int(d[1])
		} else {
			ch += int(d[1])
		}
		var mods []string
		for j, m := range tokenModifiers {
			if d[4]&(1<<j) != 0 {
				mods = append(mods, m)
			}
		}
		got[Position{Line: line, Character: ch}] = tok{tokenTypes[d[3]], mods}
	}
	tests := []struct {
		needle string
		delta  int
		typ    string
		mods   []string
	}{
		{"Cancel(reason", 0, "method", []string{"declaration", "public"}},
		{"archived()\n\n  event", 0, "event", nil},
		{"alreadyCancelled()", 0, "error", nil},
		{"phone = phoneNumber", 0, "property", []string{"personal"}},
		{"phoneNumber)\n\n  event", 0, "parameter", []string{"personal"}},
		{"reason.trim", 0, "parameter", nil},
		{"OrderStatus.cancelled", 0, "enum", nil},
		{"OrderStatus.cancelled", 12, "enumMember", nil},
		{"aggregate Order", 10, "class", []string{"declaration"}},
		{"note)", 0, "variable", nil},
		{"(admin)", 1, "type", nil},
	}
	for _, tt := range tests {
		pos := at(t, text, tt.needle, tt.delta)
		g, ok := got[pos]
		if !ok {
			t.Errorf("%q: no token", tt.needle)
			continue
		}
		if g.typ != tt.typ || !slices.Equal(g.mods, tt.mods) {
			t.Errorf("%q: %s %v, expected %s %v", tt.needle, g.typ, g.mods, tt.typ, tt.mods)
		}
	}
}

func TestExplainCommand(t *testing.T) {
	dir := copyModule(t, "shop")
	c := start(t)
	path := filepath.Join(dir, "order.rr")
	text := read(t, path)
	uri := c.open(path, text)
	c.waitDiags(uri)
	var out string
	if err := c.call("workspace/executeCommand", executeCommandParams{
		Command:   commandExplain,
		Arguments: rawArgs(t, uri, at(t, text, "Cancel(reason", 2)),
	}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Order.Cancel") || !strings.Contains(out, "alreadyCancelled") {
		t.Errorf("description:\n%s", out)
	}
	if err := c.call("workspace/executeCommand", executeCommandParams{
		Command:   commandExplain,
		Arguments: rawArgs(t, uri, Position{Line: 1, Character: 0}),
	}, &out); err == nil {
		t.Errorf("description of an empty place: no error")
	}
}

func TestLifecycle(t *testing.T) {
	c := start(t)
	if err := c.call("textDocument/unknown", struct{}{}, nil); err == nil || err.Code != codeMethodNotFound {
		t.Errorf("unknown method: %v", err)
	}
	if err := c.call("shutdown", nil, nil); err != nil {
		t.Fatal(err)
	}
	c.notify("exit", nil)
	if err := <-c.done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	c.done <- nil // for Cleanup
}

func TestNotInitialized(t *testing.T) {
	cr, sw := testPipe()
	sr, cw := testPipe()
	done := make(chan error, 1)
	go func() { done <- Serve(sr, sw, Options{}) }()
	cl := &client{t: t, conn: newConn(cr, cw), replies: map[string]chan *message{}, notes: make(chan string, 10)}
	go cl.readLoop()
	if err := cl.call("textDocument/hover", struct{}{}, nil); err == nil || err.Code != codeServerNotInitialized {
		t.Errorf("request before initialize: %v", err)
	}
	cl.notify("exit", nil)
	if err := <-done; err != errExitWithoutShutdown {
		t.Errorf("exit without shutdown: %v", err)
	}
}

// TestDelay: consecutive edits are checked once, after a pause, on the
// latest text.
func TestDelay(t *testing.T) {
	dir := copyModule(t, "shop")
	c := startWith(t, Options{Delay: 30 * time.Millisecond, Log: io.Discard})
	path := filepath.Join(dir, "order.rr")
	text := read(t, path)
	uri := c.open(path, strings.Replace(text, "cancelled(why: note)", "cancelled(why: 1)", 1))
	for v := 2; v <= 5; v++ {
		c.notify("textDocument/didChange", didChangeParams{
			TextDocument:   versionedTextDocumentIdentifier{URI: uri, Version: v},
			ContentChanges: []contentChange{{Text: text}},
		})
	}
	if diags := c.waitDiags(uri); len(diags) != 0 {
		t.Fatalf("diagnostics on the old text: %v", codes(diags))
	}
	// A request right after an edit sees the new text without waiting for the pause.
	broken := strings.Replace(text, "alreadyCancelled()", "alreadyCancelled(x: 1)", 1)
	c.notify("textDocument/didChange", didChangeParams{
		TextDocument:   versionedTextDocumentIdentifier{URI: uri, Version: 6},
		ContentChanges: []contentChange{{Text: broken}},
	})
	if got := hoverAt(t, c, uri, at(t, broken, "x: 1", 0)); got != "" {
		t.Errorf("hover for an extra argument: %q", got)
	}
	if got := hoverAt(t, c, uri, at(t, broken, "alreadyCancelled(x", 0)); !strings.Contains(got, "error alreadyCancelled") {
		t.Errorf("hover: %q", got)
	}
	if diags := c.waitDiags(uri); !slices.Contains(codes(diags), "E0204") {
		t.Errorf("diagnostics %v, expected E0204", codes(diags))
	}
}

// TestCommandNames: the VS Code LSP client registers the server's commands
// in the editor, so they must not match the extension's commands, or the
// client will not start ("command already exists").
func TestCommandNames(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "vscode", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Contributes struct {
			Commands []struct{ Command string } `json:"commands"`
		} `json:"contributes"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		t.Fatal(err)
	}
	c := start(t)
	var res initializeResult
	if err := c.call("initialize", map[string]any{"processId": nil}, &res); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range pkg.Contributes.Commands {
		if slices.Contains(res.Capabilities.ExecuteCommandProvider.Commands, cmd.Command) {
			t.Errorf("command %s exists both in the server and in the extension", cmd.Command)
		}
	}
}
