// Package lsp is the Language Server Protocol server for ReasonR
// (RFC §28): diagnostics, formatting, hover, go to
// definition and find references, completion, quick fixes, semantic tokens
// and method description (reasonr explain).
//
// The protocol is its own thin JSON-RPC layer without external
// dependencies. Documents are synchronized by changes; the module of a
// changed document is checked again Delay after the last edit. Unchanged
// files come from the parse cache.
package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultDelay is the pause after an edit before the module is checked.
const DefaultDelay = 150 * time.Millisecond

// Options are the server settings.
type Options struct {
	Delay   time.Duration // the pause before a check; 0 is DefaultDelay, < 0 no pause
	Version string        // the version for serverInfo
	Log     io.Writer     // the server error log; nil is os.Stderr
}

// Serve serves a client over streams r and w until an exit notification or
// until r closes. An error is a read or write failure or exit without shutdown.
func Serve(r io.Reader, w io.Writer, opts Options) error {
	if opts.Delay == 0 {
		opts.Delay = DefaultDelay
	}
	if opts.Log == nil {
		opts.Log = os.Stderr
	}
	s := &server{conn: newConn(r, w), opts: opts, ws: newWorkspace()}
	return s.run()
}

type server struct {
	conn *conn
	opts Options

	mu          sync.Mutex
	ws          *workspace
	initialized bool
	shutdown    bool
	timer       *time.Timer
}

var errExitWithoutShutdown = errors.New("lsp: exit without shutdown")

func (s *server) run() error {
	for {
		m, err := s.conn.read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if m.Error != nil && m.Method == "" && len(m.ID) == 0 {
			// An unreadable message: there is no one to answer.
			s.logf("%v", m.Error)
			continue
		}
		if m.Method == "exit" {
			if !s.shutdown {
				return errExitWithoutShutdown
			}
			return nil
		}
		if m.Method == "" {
			continue // a response to our request: the server sends no requests
		}
		result, rerr := s.handle(m)
		if m.isRequest() {
			if err := s.conn.reply(m.ID, result, rerr); err != nil {
				return err
			}
		} else if rerr != nil {
			s.logf("%s: %v", m.Method, rerr)
		}
	}
}

// handle handles a request or a notification; a panic in a handler becomes
// a request error rather than a server crash.
func (s *server) handle(m *message) (result any, rerr *rpcError) {
	defer func() {
		if r := recover(); r != nil {
			s.logf("panic in %s: %v\n%s", m.Method, r, debug.Stack())
			result, rerr = nil, errorf(codeInternalError, "internal server error: %v", r)
		}
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized && m.Method != "initialize" {
		if m.isRequest() {
			return nil, errorf(codeServerNotInitialized, "the server is not initialized")
		}
		return nil, nil
	}
	h, ok := handlers[m.Method]
	if !ok {
		if m.isRequest() && !strings.HasPrefix(m.Method, "$/") {
			return nil, errorf(codeMethodNotFound, "method %s is not supported", m.Method)
		}
		return nil, nil
	}
	return h(s, m.Params)
}

type handler func(s *server, params json.RawMessage) (any, *rpcError)

// handlers are the method handlers; they are called under s.mu.
var handlers map[string]handler

func init() {
	handlers = map[string]handler{
		"initialize":                       (*server).initialize,
		"initialized":                      noop,
		"shutdown":                         (*server).shutdownReq,
		"textDocument/didOpen":             (*server).didOpen,
		"textDocument/didChange":           (*server).didChange,
		"textDocument/didClose":            (*server).didClose,
		"textDocument/didSave":             (*server).didSave,
		"workspace/didChangeWatchedFiles":  (*server).didChangeWatchedFiles,
		"workspace/executeCommand":         (*server).executeCommand,
		"textDocument/formatting":          (*server).formatting,
		"textDocument/hover":               (*server).hover,
		"textDocument/definition":          (*server).definition,
		"textDocument/references":          (*server).references,
		"textDocument/completion":          (*server).completion,
		"textDocument/codeAction":          (*server).codeAction,
		"textDocument/semanticTokens/full": (*server).semanticTokens,
	}
}

func noop(*server, json.RawMessage) (any, *rpcError) { return nil, nil }

func decode(params json.RawMessage, v any) *rpcError {
	if err := json.Unmarshal(params, v); err != nil {
		return errorf(codeInvalidParams, "invalid parameters: %v", err)
	}
	return nil
}

// commandExplain is the command "describe the method at the cursor"
// (reasonr explain). Its name differs from the VS Code extension command
// reasonr.explain: the client registers the server's commands in the
// editor, and equal names conflict.
const commandExplain = "reasonr.server.explain"

func (s *server) initialize(params json.RawMessage) (any, *rpcError) {
	var p initializeParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	s.initialized = true
	return initializeResult{
		Capabilities: serverCapabilities{
			TextDocumentSync:           textDocumentSyncOptions{OpenClose: true, Change: syncIncremental, Save: true},
			HoverProvider:              true,
			DefinitionProvider:         true,
			ReferencesProvider:         true,
			DocumentFormattingProvider: true,
			CompletionProvider:         completionOptions{TriggerCharacters: []string{".", "(", ","}},
			CodeActionProvider:         codeActionOptions{CodeActionKinds: []string{"quickfix"}},
			SemanticTokensProvider: semanticTokensOptions{
				Legend: semanticTokensLegend{TokenTypes: tokenTypes, TokenModifiers: tokenModifiers},
				Full:   true,
			},
			ExecuteCommandProvider: executeCommandOptions{Commands: []string{commandExplain}},
		},
		ServerInfo: serverInfo{Name: "reasonr", Version: s.opts.Version},
	}, nil
}

func (s *server) shutdownReq(json.RawMessage) (any, *rpcError) {
	s.shutdown = true
	if s.timer != nil {
		s.timer.Stop()
	}
	return nil, nil
}

func (s *server) didOpen(params json.RawMessage) (any, *rpcError) {
	var p didOpenParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	path := pathFromURI(p.TextDocument.URI)
	if path == "" {
		return nil, nil
	}
	s.ws.docs[path] = &document{path: path, version: p.TextDocument.Version, text: []byte(p.TextDocument.Text)}
	s.changed(path)
	return nil, nil
}

func (s *server) didChange(params json.RawMessage) (any, *rpcError) {
	var p didChangeParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	d := s.ws.docs[pathFromURI(p.TextDocument.URI)]
	if d == nil {
		return nil, nil
	}
	for _, c := range p.ContentChanges {
		d.text = applyChange(d.text, c)
	}
	d.version = p.TextDocument.Version
	s.changed(d.path)
	return nil, nil
}

func (s *server) didClose(params json.RawMessage) (any, *rpcError) {
	var p didCloseParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	path := pathFromURI(p.TextDocument.URI)
	if s.ws.docs[path] != nil {
		delete(s.ws.docs, path)
		// Without a document the file on disk applies.
		s.changed(path)
	}
	return nil, nil
}

func (s *server) didSave(params json.RawMessage) (any, *rpcError) {
	var p didSaveParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	if path := pathFromURI(p.TextDocument.URI); path != "" {
		s.changed(path)
	}
	return nil, nil
}

func (s *server) didChangeWatchedFiles(params json.RawMessage) (any, *rpcError) {
	var p didChangeWatchedFilesParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	for _, c := range p.Changes {
		if path := pathFromURI(c.URI); strings.HasSuffix(path, ".rr") {
			s.changed(path)
		}
	}
	return nil, nil
}

// changed marks the file's module stale and defers the check.
func (s *server) changed(path string) {
	s.ws.module(path).dirty = true
	if s.opts.Delay < 0 {
		s.refresh()
		return
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.opts.Delay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		defer func() {
			if r := recover(); r != nil {
				s.logf("panic during a check: %v\n%s", r, debug.Stack())
			}
		}()
		if !s.shutdown {
			s.refresh()
		}
	})
}

// refresh checks stale modules and publishes their diagnostics.
func (s *server) refresh() {
	dirs := make([]string, 0, len(s.ws.modules))
	for dir, m := range s.ws.modules {
		if m.dirty {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		s.check(s.ws.modules[dir])
	}
}

// check checks a module and publishes the diagnostics of all its files.
func (s *server) check(m *moduleState) *snapshot {
	snap, err := s.ws.analyze(m, nil, "")
	if err != nil {
		s.logf("%s: %v", m.dir, err)
		return m.snap
	}
	m.snap, m.dirty = snap, false
	publish := map[string]bool{}
	for path, list := range snap.diagnostics() {
		publish[path] = true
		f := snap.files[path].Src
		out := make([]Diagnostic, 0, len(list))
		for _, d := range list {
			out = append(out, s.lspDiagnostic(snap, f, d))
		}
		s.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: uriFromPath(path), Diagnostics: out})
	}
	// Files no longer in the module: remove their diagnostics.
	for path := range m.publish {
		if !publish[path] {
			s.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: uriFromPath(path), Diagnostics: []Diagnostic{}})
		}
	}
	m.publish = publish
	return snap
}

// snapshot returns a fresh snapshot of the file's module: if there were
// edits after the last check, the module is checked at once without
// waiting for the pause.
func (s *server) snapshot(path string) *snapshot {
	m := s.ws.module(path)
	if m.dirty || m.snap == nil {
		return s.check(m)
	}
	return m.snap
}

// file returns the snapshot and the file of a document by URI; nil if the file is not in the module.
func (s *server) file(uri string) (*snapshot, *fileRef) {
	path := pathFromURI(uri)
	if path == "" || !strings.HasSuffix(path, ".rr") {
		return nil, nil
	}
	snap := s.snapshot(path)
	if snap == nil {
		return nil, nil
	}
	f := snap.files[path]
	if f == nil {
		return nil, nil
	}
	return snap, &fileRef{path: path, file: f, model: snap.model(path)}
}

func (s *server) notify(method string, params any) {
	if err := s.conn.notify(method, params); err != nil {
		s.logf("%s: %v", method, err)
	}
}

func (s *server) logf(format string, args ...any) {
	fmt.Fprintf(s.opts.Log, "reasonr lsp: "+format+"\n", args...)
}
