package lsp

import (
	"encoding/json"
	"slices"
	"sort"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/tools"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Semantic tokens: token kinds and modifiers. The standard LSP kinds are not
// enough for errors and notifications, so they have their own kinds; the
// VS Code extension declares them with parent kinds.
var (
	tokenTypes = []string{
		"class", "struct", "enum", "interface", "type", "enumMember", "property", "parameter",
		"variable", "function", "method", "event", "error", "notification", "namespace", "feature",
	}
	tokenModifiers = []string{"declaration", "public", "private", "personal", "defaultLibrary"}
)

func tokenType(name string) uint32 { return uint32(slices.Index(tokenTypes, name)) }

func tokenModifier(name string) uint32 { return 1 << slices.Index(tokenModifiers, name) }

// classify returns the kind and modifiers of the token of a name; ok ==
// false means the name is not highlighted.
func classify(t *target, id *ast.Ident) (typ, mods uint32, ok bool) {
	name := ""
	switch t.kind {
	case tDecl:
		switch t.decl.Kind {
		case types.EntityDecl, types.ActorDecl, types.ProjectionDecl:
			name = "class"
		case types.ValueDecl:
			name = "struct"
		case types.EnumDecl:
			name = "enum"
		case types.ExternalDecl:
			name = "interface"
		default:
			name = "type"
		}
	case tField:
		name = "property"
	case tParam:
		name = "parameter"
	case tLocal:
		name = "variable"
	case tBuiltin:
		name = "variable"
		if slices.Contains(builtinFuncs, t.name) {
			name = "function"
		}
		mods |= tokenModifier("defaultLibrary")
	case tEnumValue:
		name = "enumMember"
	case tRole:
		name = "type"
	case tFeature:
		name = "feature"
	case tNamespace:
		name = "namespace"
	case tMethod:
		switch t.method.Kind {
		case types.Command:
			name = "method"
		case types.Event:
			name = "event"
		case types.Error:
			name = "error"
		case types.Notification:
			name = "notification"
		default:
			name = "function"
		}
		if t.method.Kind == types.Command || t.method.Kind == types.Query {
			if t.method.Public() {
				mods |= tokenModifier("public")
			} else {
				mods |= tokenModifier("private")
			}
		}
	default:
		return 0, 0, false
	}
	if t.v != nil && t.v.Personal {
		mods |= tokenModifier("personal")
	}
	if t.def == id {
		mods |= tokenModifier("declaration")
	}
	return tokenType(name), mods, true
}

func (s *server) semanticTokens(params json.RawMessage) (any, *rpcError) {
	var p documentParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	_, ref := s.file(p.TextDocument.URI)
	if ref == nil {
		return semanticTokens{Data: []uint32{}}, nil
	}
	return semanticTokens{Data: encodeTokens(ref.file.Src, fileTokens(newResolver(ref.model), ref.file.AST))}, nil
}

type semToken struct {
	pos       source.Pos
	length    int
	typ, mods uint32
}

// fileTokens returns the tokens of a file's names in order.
func fileTokens(r *resolver, f *ast.File) []semToken {
	var out []semToken
	walkIdents(f, func(path []ast.Node) {
		id := last(path).(*ast.Ident)
		t := r.resolve(path)
		if t == nil {
			return
		}
		if typ, mods, ok := classify(t, id); ok {
			out = append(out, semToken{pos: id.Pos(), length: len(id.Name), typ: typ, mods: mods})
		}
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].pos < out[j].pos })
	return out
}

// encodeTokens encodes tokens in the relative LSP format: the line and the
// start relative to the previous token, the length in UTF-16 code units.
func encodeTokens(f *source.File, toks []semToken) []uint32 {
	data := make([]uint32, 0, 5*len(toks))
	prev := Position{}
	for _, t := range toks {
		start := lspPos(f, t.pos)
		end := lspPos(f, t.pos+source.Pos(t.length))
		ch := start.Character
		if start.Line == prev.Line {
			ch -= prev.Character
		}
		data = append(data, uint32(start.Line-prev.Line), uint32(ch), uint32(end.Character-start.Character), t.typ, t.mods)
		prev = start
	}
	return data
}

// executeCommand runs server commands: reasonr.server.explain(uri,
// position) returns a description of the method or type at the cursor
// (reasonr explain).
func (s *server) executeCommand(params json.RawMessage) (any, *rpcError) {
	var p executeCommandParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	if p.Command != commandExplain {
		return nil, errorf(codeInvalidParams, "unknown command %s", p.Command)
	}
	var uri string
	var pos Position
	if len(p.Arguments) != 2 || json.Unmarshal(p.Arguments[0], &uri) != nil || json.Unmarshal(p.Arguments[1], &pos) != nil {
		return nil, errorf(codeInvalidParams, "%s: uri and position arguments are needed", commandExplain)
	}
	_, ref := s.file(uri)
	if ref == nil {
		return nil, errorf(codeRequestFailed, "file %s is not in the module", uri)
	}
	_, t := newResolver(ref.model).at(ref.file.AST, filePos(ref.file.Src, pos))
	name := ""
	switch {
	case t == nil:
	case t.kind == tMethod && t.decl != nil:
		name = t.decl.Name + "." + t.method.Name
	case t.kind == tParam && t.method != nil && t.decl != nil:
		name = t.decl.Name + "." + t.method.Name
	case t.decl != nil:
		name = t.decl.Name
	}
	if name == "" {
		return nil, errorf(codeRequestFailed, "no type or method at the cursor")
	}
	text, err := tools.Explain(ref.model, name)
	if err != nil {
		return nil, errorf(codeRequestFailed, "%v", err)
	}
	return text, nil
}
