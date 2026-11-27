package lsp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
)

// syncBuffer is the server log for checking panics.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestExamples opens every example file and requests hover and definition
// for every word, completion after every dot and at the start of every
// line, semantic tokens and formatting. There must be no errors or panics;
// the examples have no error diagnostics.
func TestExamples(t *testing.T) {
	if testing.Short() {
		t.Skip("long test")
	}
	root, err := filepath.Abs(rfcblocks.Examples())
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".rr") {
			files = append(files, p)
		}
		return err
	})
	log := &syncBuffer{}
	c := startWith(t, Options{Delay: -1, Log: log})
	for _, path := range files {
		text := read(t, path)
		uri := c.open(path, text)
		for _, d := range c.waitDiags(uri) {
			if d.Severity == severityError {
				t.Errorf("%s: %s %s", path, d.Code, d.Message)
			}
		}
		doc := textDocumentIdentifier{URI: uri}
		var toks semanticTokens
		if err := c.call("textDocument/semanticTokens/full", documentParams{TextDocument: doc}, &toks); err != nil {
			t.Errorf("%s: semanticTokens: %v", path, err)
		}
		var edits []TextEdit
		if err := c.call("textDocument/formatting", documentParams{TextDocument: doc}, &edits); err != nil || len(edits) != 0 {
			t.Errorf("%s: formatting: %v %+v", path, err, edits)
		}
		b := []byte(text)
		for off, r := range text {
			prev := ' '
			if off > 0 {
				prev = rune(text[off-1])
			}
			pos := textPos(b, off)
			p := textDocumentPositionParams{TextDocument: doc, Position: pos}
			if unicode.IsLetter(r) && !unicode.IsLetter(prev) && prev != '_' {
				var h *hover
				if err := c.call("textDocument/hover", p, &h); err != nil {
					t.Errorf("%s:%d:%d: hover: %v", path, pos.Line+1, pos.Character+1, err)
				}
				var locs []Location
				if err := c.call("textDocument/definition", p, &locs); err != nil {
					t.Errorf("%s:%d:%d: definition: %v", path, pos.Line+1, pos.Character+1, err)
				}
			}
			if prev == '.' || prev == '\n' {
				var list completionList
				if err := c.call("textDocument/completion", p, &list); err != nil {
					t.Errorf("%s:%d:%d: completion: %v", path, pos.Line+1, pos.Character+1, err)
				}
			}
		}
	}
	if strings.Contains(log.String(), "panic") {
		t.Errorf("server log:\n%s", log.String())
	}
}

// TestDefinitionFake: go to the definition of a model command from a fake
// ([orderId].DriverFound) and of a fake command from a test (Matching.Setup).
func TestDefinitionFake(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(rfcblocks.Examples(), "taxi"))
	if err != nil {
		t.Fatal(err)
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatal(err)
	}
	c := start(t)
	for _, tt := range []struct{ file, needle, defFile, def string }{
		{"contexts/Dispatch/matching.fake.rr", "DriverFound(driverId: d)", "contexts/Dispatch/trip.rr", "DriverFound(driverId Driver.Id)"},
		{"contexts/Ordering/ride.test.rr", "Setup(freeDrivers: [d1])", "contexts/Dispatch/matching.fake.rr", "Setup(freeDrivers list<Driver.Id>)"},
	} {
		path := filepath.Join(root, filepath.FromSlash(tt.file))
		text := read(t, path)
		uri := c.open(path, text)
		c.waitDiags(uri)
		var locs []Location
		if err := c.call("textDocument/definition", textDocumentPositionParams{
			TextDocument: textDocumentIdentifier{URI: uri}, Position: at(t, text, tt.needle, 2),
		}, &locs); err != nil {
			t.Fatal(err)
		}
		defPath := filepath.Join(root, filepath.FromSlash(tt.defFile))
		want := at(t, read(t, defPath), tt.def, 0)
		if len(locs) != 1 || locs[0].URI != uriFromPath(defPath) || locs[0].Range.Start != want {
			t.Errorf("%s %q: definition %+v, expected %s %+v", tt.file, tt.needle, locs, tt.defFile, want)
		}
	}
}

// TestDefinitionQualified: a full type path (Ordering.CarClass) goes to the
// type declaration of another bounded context, a part of the path
// (Ordering) to the bounded context.
func TestDefinitionQualified(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(rfcblocks.Examples(), "taxi"))
	if err != nil {
		t.Fatal(err)
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatal(err)
	}
	c := start(t)
	path := filepath.Join(root, "contexts", "Dispatch", "driver.rr")
	text := read(t, path)
	uri := c.open(path, text)
	c.waitDiags(uri)
	for _, tt := range []struct{ needle, defFile, def string }{
		{"CarClass, pageReq", "contexts/Dispatch/types.rr", "CarClass\n"},
	} {
		var locs []Location
		if err := c.call("textDocument/definition", textDocumentPositionParams{
			TextDocument: textDocumentIdentifier{URI: uri}, Position: at(t, text, tt.needle, 1),
		}, &locs); err != nil {
			t.Fatal(err)
		}
		defPath := filepath.Join(root, filepath.FromSlash(tt.defFile))
		if tt.needle[0] == 'O' {
			// A part of the path is a bounded context: the first declaration of its files.
			if len(locs) != 1 || !strings.Contains(string(locs[0].URI), "Ordering") {
				t.Errorf("%q: %+v", tt.needle, locs)
			}
			continue
		}
		want := at(t, read(t, defPath), tt.def, 0)
		if len(locs) != 1 || locs[0].URI != uriFromPath(defPath) || locs[0].Range.Start != want {
			t.Errorf("%q: %+v, expected %s %+v", tt.needle, locs, tt.defFile, want)
		}
	}
}

// TestDefinitionOnPath: on Ordering.Order.placed in an ACL goes to the
// entity event of the source bounded context; Order to its declaration.
func TestDefinitionOnPath(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(rfcblocks.Examples(), "taxi"))
	if err != nil {
		t.Fatal(err)
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatal(err)
	}
	c := start(t)
	path := filepath.Join(root, "contexts", "Dispatch", "ordering.acl.rr")
	text := read(t, path)
	uri := c.open(path, text)
	c.waitDiags(uri)
	ordering := filepath.Join(root, "contexts", "Ordering", "order.rr")
	for _, tt := range []struct{ needle, def string }{
		{"placed\n    [Trip", "event placed("},
		{"Order.placed\n", "aggregate Order"},
	} {
		var locs []Location
		if err := c.call("textDocument/definition", textDocumentPositionParams{
			TextDocument: textDocumentIdentifier{URI: uri}, Position: at(t, text, tt.needle, 1),
		}, &locs); err != nil {
			t.Fatal(err)
		}
		want := at(t, read(t, ordering), tt.def, strings.Index(tt.def, " ")+1)
		if len(locs) != 1 || locs[0].URI != uriFromPath(ordering) || locs[0].Range.Start != want {
			t.Errorf("%q: %+v, expected %+v", tt.needle, locs, want)
		}
	}
}

// TestFeatureFlag: the taxi flag tips goes from a read in a command and from
// a test's feature line to the declaration in types.rr; hover shows the
// date and the description.
func TestFeatureFlag(t *testing.T) {
	root, err := filepath.Abs(filepath.Join(rfcblocks.Examples(), "taxi"))
	if err != nil {
		t.Fatal(err)
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatal(err)
	}
	c := start(t)
	defPath := filepath.Join(root, "contexts", "Ordering", "types.rr")
	want := at(t, read(t, defPath), "tips \"2026", 0)
	for _, tt := range []struct{ file, needle string }{
		{"contexts/Ordering/order.rr", "tips\n      if status"},
		{"contexts/Ordering/ride.test.rr", "tips\n  let c1"},
		{"contexts/Ordering/ui.rr", "tips and status"},
	} {
		path := filepath.Join(root, filepath.FromSlash(tt.file))
		text := read(t, path)
		uri := c.open(path, text)
		c.waitDiags(uri)
		pos := at(t, text, tt.needle, 1)
		var locs []Location
		if err := c.call("textDocument/definition", textDocumentPositionParams{
			TextDocument: textDocumentIdentifier{URI: uri}, Position: pos,
		}, &locs); err != nil {
			t.Fatal(err)
		}
		if len(locs) != 1 || locs[0].URI != uriFromPath(defPath) || locs[0].Range.Start != want {
			t.Errorf("%s: definition %+v, expected %+v", tt.file, locs, want)
		}
		if got := hoverAt(t, c, uri, pos); !strings.Contains(got, `feature Ordering.tips "2026-11-07"`) || !strings.Contains(got, "Tips for the driver") {
			t.Errorf("%s: hover %q", tt.file, got)
		}
	}
}
