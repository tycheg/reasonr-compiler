package lsp

import "encoding/json"

// LSP 3.17 protocol structures: only what the server uses.

// Position is a position in a document: a line and a character from 0; the
// character counts UTF-16 code units (the default LSP position encoding).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open interval [Start, End).
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a place in a document.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextEdit replaces a range with text.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type initializeParams struct {
	ProcessID *int   `json:"processId"`
	RootURI   string `json:"rootUri"`
}

type initializeResult struct {
	Capabilities serverCapabilities `json:"capabilities"`
	ServerInfo   serverInfo         `json:"serverInfo"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type serverCapabilities struct {
	TextDocumentSync           textDocumentSyncOptions `json:"textDocumentSync"`
	HoverProvider              bool                    `json:"hoverProvider"`
	DefinitionProvider         bool                    `json:"definitionProvider"`
	ReferencesProvider         bool                    `json:"referencesProvider"`
	DocumentFormattingProvider bool                    `json:"documentFormattingProvider"`
	CompletionProvider         completionOptions       `json:"completionProvider"`
	CodeActionProvider         codeActionOptions       `json:"codeActionProvider"`
	SemanticTokensProvider     semanticTokensOptions   `json:"semanticTokensProvider"`
	ExecuteCommandProvider     executeCommandOptions   `json:"executeCommandProvider"`
}

// Document synchronization: opening and closing, changes by range, saving.
type textDocumentSyncOptions struct {
	OpenClose bool `json:"openClose"`
	Change    int  `json:"change"`
	Save      bool `json:"save"`
}

const syncIncremental = 2

type completionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters"`
}

type codeActionOptions struct {
	CodeActionKinds []string `json:"codeActionKinds"`
}

type semanticTokensOptions struct {
	Legend semanticTokensLegend `json:"legend"`
	Full   bool                 `json:"full"`
}

type semanticTokensLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

type executeCommandOptions struct {
	Commands []string `json:"commands"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// contentChange is a document change: Range == nil means the whole text.
type contentChange struct {
	Range *Range `json:"range"`
	Text  string `json:"text"`
}

type didChangeParams struct {
	TextDocument   versionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []contentChange                 `json:"contentChanges"`
}

type didCloseParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type didSaveParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type fileEvent struct {
	URI string `json:"uri"`
}

type didChangeWatchedFilesParams struct {
	Changes []fileEvent `json:"changes"`
}

type textDocumentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type referenceParams struct {
	textDocumentPositionParams
	Context struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

type documentParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

// Diagnostic is an LSP diagnostic.
type Diagnostic struct {
	Range              Range                `json:"range"`
	Severity           int                  `json:"severity"`
	Code               string               `json:"code"`
	Source             string               `json:"source"`
	Message            string               `json:"message"`
	RelatedInformation []relatedInformation `json:"relatedInformation,omitempty"`
}

const (
	severityError   = 1
	severityWarning = 2
)

type relatedInformation struct {
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type markupContent struct {
	Kind  string `json:"kind"` // markdown
	Value string `json:"value"`
}

type hover struct {
	Contents markupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// CompletionItem is a completion candidate.
type CompletionItem struct {
	Label            string         `json:"label"`
	Kind             int            `json:"kind,omitempty"`
	Detail           string         `json:"detail,omitempty"`
	Documentation    *markupContent `json:"documentation,omitempty"`
	SortText         string         `json:"sortText,omitempty"`
	InsertText       string         `json:"insertText,omitempty"`
	InsertTextFormat int            `json:"insertTextFormat,omitempty"`
}

// Completion candidate kinds (CompletionItemKind).
const (
	kindMethod     = 2
	kindFunction   = 3
	kindField      = 5
	kindVariable   = 6
	kindClass      = 7
	kindInterface  = 8
	kindProperty   = 10
	kindEnum       = 13
	kindKeyword    = 14
	kindEnumMember = 20
	kindStruct     = 22
	kindEvent      = 23
)

type completionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type codeActionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
}

type codeAction struct {
	Title       string        `json:"title"`
	Kind        string        `json:"kind"`
	Diagnostics []Diagnostic  `json:"diagnostics,omitempty"`
	IsPreferred bool          `json:"isPreferred,omitempty"`
	Edit        workspaceEdit `json:"edit"`
}

type workspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

type semanticTokens struct {
	Data []uint32 `json:"data"`
}

type executeCommandParams struct {
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments"`
}
