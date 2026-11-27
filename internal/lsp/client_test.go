package lsp

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// client is a test LSP client: the server runs in a goroutine over pipes.
type client struct {
	t      *testing.T
	conn   *conn
	nextID int
	done   chan error

	mu      sync.Mutex
	replies map[string]chan *message
	diags   map[string][]Diagnostic // the latest diagnostics by URI
	notes   chan string             // URIs of published diagnostics
}

// start starts and initializes the server. The module is checked right
// after an edit (Delay < 0) so the tests are deterministic.
func start(t *testing.T) *client {
	t.Helper()
	return startWith(t, Options{Delay: -1, Log: io.Discard})
}

func startWith(t *testing.T, opts Options) *client {
	t.Helper()
	cr, sw := io.Pipe() // server → client
	sr, cw := io.Pipe() // client → server
	c := &client{
		t: t, conn: newConn(cr, cw), done: make(chan error, 1),
		replies: map[string]chan *message{}, diags: map[string][]Diagnostic{}, notes: make(chan string, 1000),
	}
	go func() {
		err := Serve(sr, sw, opts)
		sw.Close()
		c.done <- err
	}()
	go c.readLoop()
	t.Cleanup(func() {
		cw.Close()
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			t.Error("the server did not stop")
		}
	})
	var res initializeResult
	c.call("initialize", map[string]any{"processId": nil, "rootUri": nil}, &res)
	c.notify("initialized", struct{}{})
	return c
}

func (c *client) readLoop() {
	for {
		m, err := c.conn.read()
		if err != nil {
			return
		}
		if len(m.ID) > 0 && m.Method == "" {
			c.mu.Lock()
			ch := c.replies[string(m.ID)]
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		if m.Method == "textDocument/publishDiagnostics" {
			var p publishDiagnosticsParams
			if err := json.Unmarshal(m.Params, &p); err == nil {
				c.mu.Lock()
				c.diags[p.URI] = p.Diagnostics
				c.mu.Unlock()
				c.notes <- p.URI
			}
		}
	}
}

// call sends a request and decodes the result into result; it returns a
// response error.
func (c *client) call(method string, params, result any) *rpcError {
	c.t.Helper()
	c.nextID++
	id := json.RawMessage(strconv.Itoa(c.nextID))
	ch := make(chan *message, 1)
	c.mu.Lock()
	c.replies[string(id)] = ch
	c.mu.Unlock()
	b, err := json.Marshal(params)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := c.conn.write(&message{ID: id, Method: method, Params: b}); err != nil {
		c.t.Fatal(err)
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if result != nil {
			if err := json.Unmarshal(m.Result, result); err != nil {
				c.t.Fatalf("%s: %v: %s", method, err, m.Result)
			}
		}
		return nil
	case <-time.After(10 * time.Second):
		c.t.Fatalf("%s: no response", method)
	}
	return nil
}

func (c *client) notify(method string, params any) {
	c.t.Helper()
	if err := c.conn.notify(method, params); err != nil {
		c.t.Fatal(err)
	}
}

// waitDiags waits for diagnostics to be published for uri and returns them.
func (c *client) waitDiags(uri string) []Diagnostic {
	c.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case u := <-c.notes:
			if u == uri {
				c.mu.Lock()
				defer c.mu.Unlock()
				return c.diags[uri]
			}
		case <-deadline:
			c.t.Fatalf("no diagnostics for %s", uri)
		}
	}
}

func (c *client) open(path, text string) string {
	uri := uriFromPath(path)
	c.notify("textDocument/didOpen", didOpenParams{TextDocument: textDocumentItem{URI: uri, LanguageID: "reasonr", Version: 1, Text: text}})
	return uri
}

// copyModule copies module testdata/<name> into a temporary directory.
func copyModule(t *testing.T, name string) string {
	t.Helper()
	dst := t.TempDir()
	// A directory without symlinks: URI paths match the server's paths.
	dst, err := filepath.EvalSymlinks(dst)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join("testdata", name)
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// at returns the LSP position of the first occurrence of needle in text,
// shifted by delta bytes.
func at(t *testing.T, text, needle string, delta int) Position {
	t.Helper()
	i := strings.Index(text, needle)
	if i < 0 {
		t.Fatalf("no %q in the text", needle)
	}
	return textPos([]byte(text), i+delta)
}

func codes(list []Diagnostic) []string {
	var out []string
	for _, d := range list {
		out = append(out, d.Code)
	}
	return out
}

func rawArgs(t *testing.T, args ...any) []json.RawMessage {
	t.Helper()
	var out []json.RawMessage
	for _, a := range args {
		b, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func testPipe() (*io.PipeReader, *io.PipeWriter) { return io.Pipe() }
