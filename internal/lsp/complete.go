package lsp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// placeholder is inserted at the cursor if a name has not started yet: so
// "order." and "placed(" parse as expressions with a name, and type
// checking gives the type of what stands before the dot and the called method.
const placeholder = "_"

// Keywords by place (RFC §6.4).
var (
	topKeywords = []string{"entity", "actor", "value", "enum", "projection", "external", "role", "app",
		"screen", "primitive", "default", "texts", "migration", "test", "extend", "exclude", "let"}
	memberKeywords = []string{"id", "index", "command", "event", "notification", "error", "query", "rule",
		"request", "on", "check"}
	stmtKeywords = []string{"let", "if", "else", "for", "while", "break", "continue", "return", "delete", "forget"}
	exprKeywords = []string{"and", "or", "not", "is", "none", "in", "true", "false", "new"}
	testKeywords = []string{"given", "when", "then", "time", "passes"}
	typeKeywords = []string{"int", "bigint", "float", "decimal", "string", "bool", "uuid", "date", "time",
		"timestamp", "duration", "bytes", "list", "set", "map", "page", "Timer"}
)

// Built-in values and functions (RFC §8.3).
var (
	builtinValues = []string{"id", "now", "today", "after", "hasErrors", "me", "caller"}
	builtinFuncs  = []string{"int", "bigint", "float", "decimal", "string", "abs", "floor", "ceil",
		"min", "max", "round", "schedule"}
)

func (s *server) completion(params json.RawMessage) (any, *rpcError) {
	var p textDocumentPositionParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	path := pathFromURI(p.TextDocument.URI)
	text, ok := s.text(path)
	if !ok {
		return completionList{Items: []CompletionItem{}}, nil
	}
	off := textOffset(text, p.Position)
	start := wordStart(text, off)
	lineStart := start
	for lineStart > 0 && text[lineStart-1] != '\n' {
		lineStart--
	}
	atLineStart := strings.TrimSpace(string(text[lineStart:start])) == ""

	var ref *fileRef
	if start == off {
		// No name yet: check the text with a placeholder, leaving the module snapshot alone.
		patched := make([]byte, 0, len(text)+len(placeholder))
		patched = append(append(append(patched, text[:off]...), placeholder...), text[off:]...)
		m := s.ws.module(path)
		snap, err := s.ws.analyze(m, map[string][]byte{path: patched}, extensionOf(m.dir, path))
		if err != nil {
			s.logf("%s: %v", path, err)
		} else if f := snap.files[path]; f != nil {
			ref = &fileRef{path: path, file: f, model: snap.base}
		}
	} else {
		_, ref = s.file(p.TextDocument.URI)
	}
	if ref == nil {
		return completionList{Items: []CompletionItem{}}, nil
	}
	c := &completer{s: s, r: newResolver(ref.model), src: ref.file.Src, seen: map[string]bool{}}
	pos := ref.file.Src.Pos(start)
	c.complete(nodesAt(ref.file.AST, pos), pos, atLineStart, start == lineStart)
	if c.items == nil {
		c.items = []CompletionItem{}
	}
	return completionList{Items: c.items}, nil
}

// wordStart returns the start of the name that ends at off.
func wordStart(text []byte, off int) int {
	for off > 0 {
		r, size := utf8.DecodeLastRune(text[:off])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		off -= size
	}
	return off
}

type completer struct {
	s     *server
	r     *resolver
	src   *source.File
	items []CompletionItem
	seen  map[string]bool
}

// system is the system of the file being completed; "" outside bounded contexts.
func (c *completer) system() string {
	for _, f := range c.r.m.Program.Files {
		if f.Src == c.src {
			sys, _, _ := strings.Cut(f.Context, ".")
			return sys
		}
	}
	return ""
}

func (c *completer) add(it CompletionItem) {
	if it.Label == placeholder || c.seen[it.Label] {
		return
	}
	c.seen[it.Label] = true
	c.items = append(c.items, it)
}

// complete picks candidates for the cursor place: the node path up to the
// position where the name starts.
func (c *completer) complete(path []ast.Node, pos source.Pos, atLineStart, atColumn0 bool) {
	info := c.r.info
	if id, ok := last(path).(*ast.Ident); ok {
		switch p := parent(path, 1).(type) {
		case *ast.SelectorExpr:
			if p.Sel == id {
				c.members(p.X)
				return
			}
		case *ast.Arg:
			call := parent(path, 3)
			switch {
			case p.Name == id:
				c.argNames(call, id)
				return
			case p.Name == nil:
				// A positional argument: most likely an argument name is being typed.
				if c.argNames(call, id) {
					return
				}
			default:
				c.expected(c.argType(path))
			}
		case *ast.BinaryExpr:
			if p.Op == token.EQ || p.Op == token.NE {
				other := p.X
				if p.X == id {
					other = p.Y
				}
				c.expected(info.Types[other])
			}
		case *ast.AssignStmt:
			if p.Value == id {
				c.expected(info.Types[p.Target])
			}
		case *ast.LetStmt:
			if p.Value == id && p.Type != nil {
				c.expected(info.TypeExprs[p.Type])
			}
		}
	}
	for i := len(path) - 1; i >= 0; i-- {
		if se, ok := path[i].(*ast.SearchExpr); ok && se.Where != nil && se.Where.Pos() <= pos && pos <= se.Where.End() {
			c.searchFields(se)
		}
	}
	c.general(path, pos, atLineStart, atColumn0)
}

// members, after "x." and "x?.": fields, methods, enum values, entity
// commands, type queries, built-in properties and methods.
func (c *completer) members(x ast.Expr) {
	info := c.r.info
	if id, ok := x.(*ast.Ident); ok && info.Uses[id] == nil {
		if d := info.Lookup(id.Name); d != nil {
			for _, v := range d.Values {
				c.add(CompletionItem{Label: v.Name, Kind: kindEnumMember, Detail: d.Name, Documentation: c.doc(v.Node.Doc)})
			}
			if d.ID != nil {
				c.add(CompletionItem{Label: "Id", Kind: kindClass, Detail: d.ID.String()})
			}
			for _, md := range d.Methods {
				if md.Kind == types.Query || md.Kind == types.Request {
					c.method(d, md)
				}
			}
			return
		}
	}
	switch t := types.Unwrap(info.Types[x]).(type) {
	case *types.Ref:
		for _, md := range t.Owner.Methods {
			if md.Kind == types.Command {
				c.method(t.Owner, md)
			}
		}
		return
	case *types.Value:
		c.fields(t.Decl, false)
		for _, md := range t.Decl.Methods {
			if md.Kind == types.ValueMethod {
				c.method(t.Decl, md)
			}
		}
		return
	case *types.Record:
		c.fields(t.Owner, true)
		return
	case nil:
		return
	default:
		for _, b := range sema.BuiltinMembers(t) {
			it := CompletionItem{Label: b.Name, Kind: kindProperty, Detail: typeString(b.Type)}
			if b.Method {
				it.Kind = kindMethod
				params := make([]string, len(b.Params))
				for i, p := range b.Params {
					params[i] = p.String()
				}
				it.Detail = "(" + strings.Join(params, ", ") + ") " + typeString(b.Type)
				it.InsertText = b.Name + "($0)"
				if len(b.Params) == 0 {
					it.InsertText = b.Name + "()"
				}
				it.InsertTextFormat = insertSnippet
			}
			c.add(it)
		}
	}
}

const insertSnippet = 2

func (c *completer) fields(d *types.Decl, withID bool) {
	if withID && d.ID != nil {
		c.add(CompletionItem{Label: "id", Kind: kindField, Detail: d.ID.String()})
	}
	for _, f := range d.Fields {
		it := CompletionItem{Label: f.Name, Kind: kindField, Detail: typeString(f.Type)}
		if fd, ok := f.Node.(*ast.FieldDecl); ok {
			it.Documentation = c.doc(fd.Doc)
		}
		c.add(it)
	}
}

// method adds a method call with a snippet of the required arguments.
func (c *completer) method(d *types.Decl, md *types.Method) {
	kind := kindMethod
	switch md.Kind {
	case types.Event:
		kind = kindEvent
	case types.Query, types.Rule, types.ValueMethod, types.Request:
		kind = kindFunction
	}
	var args []string
	for _, p := range md.Params {
		if n, ok := p.Node.(*ast.Param); ok && n.Default != nil {
			continue
		}
		if _, opt := p.Type.(*types.Optional); opt {
			continue
		}
		args = append(args, fmt.Sprintf("%s: ${%d}", p.Name, len(args)+1))
	}
	insert := md.Name + "(" + strings.Join(args, ", ") + ")"
	if len(args) == 0 {
		insert += "$0"
	}
	sortText := "1"
	if md.Public() {
		sortText = "0"
	}
	c.add(CompletionItem{
		Label: md.Name, Kind: kind, Detail: c.r.m.Source(memberHeader(md.Node)),
		Documentation: c.doc(docOf(md.Node)), InsertText: insert, InsertTextFormat: insertSnippet,
		SortText: sortText + md.Name,
	})
}

// argNames adds the names of call arguments not passed yet; false means
// the call takes no named arguments.
func (c *completer) argNames(call ast.Node, cur *ast.Ident) bool {
	var params []*types.Var
	var args *ast.ArgList
	info := c.r.info
	switch n := call.(type) {
	case *ast.CallExpr:
		args = n.Args
		cl := info.Calls[n]
		if cl == nil {
			return false
		}
		switch {
		case cl.Kind == sema.CallConstruct || cl.Kind == sema.CallUpsert:
			params = cl.Decl.Fields
		case cl.Method != nil:
			params = cl.Method.Params
		default:
			return false
		}
	case *ast.Binding:
		args, params = n.Args, c.r.uiArgParams(n)
	case *ast.PrimNode:
		args, params = n.Args, c.r.uiArgParams(n)
	case *ast.OnNode:
		args, params = n.Args, c.r.uiArgParams(n)
	}
	if len(params) == 0 {
		return false
	}
	given := map[string]bool{}
	if args != nil {
		for _, a := range args.Args {
			if a.Name != nil && a.Name != cur {
				given[a.Name.Name] = true
			}
		}
	}
	for i, p := range params {
		if given[p.Name] {
			continue
		}
		c.add(CompletionItem{
			Label: p.Name + ":", Kind: kindProperty, Detail: typeString(p.Type),
			InsertText: p.Name + ": ", SortText: fmt.Sprintf("0%03d", i),
		})
	}
	return true
}

// argType returns the type of the parameter whose value is being typed.
func (c *completer) argType(path []ast.Node) types.Type {
	a := parent(path, 1).(*ast.Arg)
	argPath := append(append([]ast.Node(nil), path[:len(path)-1]...), a.Name)
	if t := c.r.resolve(argPath); t != nil {
		return t.typ
	}
	return nil
}

// expected adds values of the expected type: enums and bool.
func (c *completer) expected(t types.Type) {
	switch t := types.Unwrap(t).(type) {
	case *types.Enum:
		for _, v := range t.Decl.Values {
			label := t.Decl.Name + "." + v.Name
			c.add(CompletionItem{Label: label, Kind: kindEnumMember, Detail: t.Decl.Name, SortText: "0" + label,
				Documentation: c.doc(v.Node.Doc)})
		}
	case *types.Basic:
		if t.Kind == types.Bool {
			c.add(CompletionItem{Label: "true", Kind: kindKeyword, SortText: "0true"})
			c.add(CompletionItem{Label: "false", Kind: kindKeyword, SortText: "0false"})
		}
	}
}

// searchFields, in a search where: the record fields; fields that start an
// index come first and with the index name.
func (c *completer) searchFields(se *ast.SearchExpr) {
	d := c.r.info.Lookup(se.Type.Name)
	if d == nil {
		return
	}
	first := map[string]string{}
	for _, ix := range d.Indexes {
		if len(ix.Node.Keys) > 0 {
			name := ix.Node.Keys[0].Path.Elems[0].Name
			if _, ok := first[name]; !ok {
				first[name] = ix.Name
			}
		}
	}
	add := func(name string, t types.Type) {
		it := CompletionItem{Label: name, Kind: kindField, Detail: typeString(t), SortText: "1" + name}
		if ix, ok := first[name]; ok {
			it.Detail += " · index " + ix
			it.SortText = "0" + name
		}
		c.add(it)
	}
	if d.ID != nil {
		add("id", d.ID)
	}
	for _, f := range d.Fields {
		add(f.Name, f.Type)
	}
}

// general lists the names visible at the cursor and the keywords for the place.
func (c *completer) general(path []ast.Node, pos source.Pos, atLineStart, atColumn0 bool) {
	var member ast.Member
	var decl ast.Decl
	var block, typePos bool
	for _, n := range path {
		switch n := n.(type) {
		case *ast.TestDecl:
			decl, block = n, true
		case ast.Decl:
			decl = n
		case *ast.BadMember:
		case ast.Member:
			member = n
		case *ast.Block, *ast.GivenBlock, *ast.WhenBlock, *ast.ThenBlock:
			block = true
		case ast.TypeExpr:
			typePos = true
		}
	}
	switch {
	case typePos:
		c.typeNames()
		c.keywords(typeKeywords)
		return
	case atColumn0 || decl == nil:
		if atLineStart {
			c.keywords(topKeywords)
		}
		return
	case !block:
		switch d := decl.(type) {
		case *ast.ScreenDecl, *ast.AppDecl:
			if atLineStart {
				c.layout(path, c.r.m.Module.FileSet.Position(pos).Line)
				return
			}
			if s, ok := d.(*ast.ScreenDecl); ok {
				c.screenParams(s)
			}
			c.typeNames()
			return
		}
		if atLineStart {
			if hasMembers(decl) {
				c.keywords(memberKeywords)
			}
			return
		}
		// Access lists, default values.
		c.typeNames()
		for name := range c.r.info.Roles {
			c.add(CompletionItem{Label: name, Kind: kindVariable, Detail: "role"})
		}
		return
	}
	if atLineStart {
		c.keywords(stmtKeywords)
		if _, ok := decl.(*ast.TestDecl); ok {
			c.keywords(testKeywords)
		}
	}
	c.locals(path, pos)
	if member != nil {
		if owner := c.r.info.Owner[member]; owner != nil {
			c.ownerNames(owner, member)
		}
	}
	if _, ok := decl.(*ast.TestDecl); ok {
		c.fixtures()
	}
	c.typeNames()
	for _, name := range builtinValues {
		c.add(CompletionItem{Label: name, Kind: kindVariable, Detail: "builtin", SortText: "8" + name})
	}
	for full, f := range c.r.info.Features {
		// A flag of its own system by short name, of another by full name.
		label := f.Name.Name
		if c.r.info.Features[label] != f && !strings.HasPrefix(full, c.system()+".") {
			label = full
		}
		c.add(CompletionItem{Label: label, Kind: kindVariable, Detail: "feature " + f.Date.Raw, SortText: "7" + label})
	}
	for _, name := range builtinFuncs {
		c.add(CompletionItem{Label: name, Kind: kindFunction, Detail: "builtin", SortText: "8" + name})
	}
	if !atLineStart {
		c.keywords(exprKeywords)
	}
}

func (c *completer) screenParams(v *ast.ScreenDecl) {
	if d := c.r.info.Lookup(v.Name.Name); d != nil {
		for _, p := range d.Params {
			c.add(CompletionItem{Label: p.Name, Kind: kindVariable, Detail: typeString(p.Type), SortText: "2" + p.Name})
		}
	}
}

// fixtures are the shared test lets.
func (c *completer) fixtures() {
	for _, d := range c.r.m.Program.Decls {
		if l, ok := d.(*ast.LetDecl); ok && l.Let != nil && l.Let.Name != nil {
			c.add(CompletionItem{Label: l.Let.Name.Name, Kind: kindVariable, SortText: "2" + l.Let.Name.Name})
		}
	}
}

func hasMembers(d ast.Decl) bool {
	switch d.(type) {
	case *ast.EntityDecl, *ast.ProjectionDecl, *ast.ValueDecl, *ast.ExternalDecl, *ast.ExtendDecl:
		return true
	}
	return false
}

func (c *completer) keywords(list []string) {
	for _, k := range list {
		c.add(CompletionItem{Label: k, Kind: kindKeyword, SortText: "9" + k})
	}
}

// locals adds the let and for variables declared before the cursor in the
// enclosing blocks, and the method parameters.
func (c *completer) locals(path []ast.Node, pos source.Pos) {
	info := c.r.info
	for i, n := range path {
		switch n := n.(type) {
		case *ast.Block:
			for _, st := range n.List {
				if st.Pos() >= pos {
					break
				}
				if let, ok := st.(*ast.LetStmt); ok && let.Name.Name != placeholder {
					t := info.Types[let.Value]
					if let.Type != nil {
						t = info.TypeExprs[let.Type]
					}
					c.add(CompletionItem{Label: let.Name.Name, Kind: kindVariable, Detail: typeString(t), SortText: "2" + let.Name.Name})
				}
			}
		case *ast.ForStmt:
			if i+1 < len(path) && path[i+1] == n.Body {
				var t types.Type
				switch x := types.Unwrap(info.Types[n.X]).(type) {
				case *types.List:
					t = x.Elem
				case *types.Set:
					t = x.Elem
				}
				c.add(CompletionItem{Label: n.Var.Name, Kind: kindVariable, Detail: typeString(t), SortText: "2" + n.Var.Name})
			}
		case *ast.TestDecl:
			for _, st := range n.Setup {
				if let, ok := st.(*ast.LetStmt); ok && st.Pos() < pos {
					c.add(CompletionItem{Label: let.Name.Name, Kind: kindVariable, Detail: typeString(info.Types[let.Value]), SortText: "2" + let.Name.Name})
				}
			}
		case *ast.ParamList:
			// The cursor is in a parameter declaration: no names are offered.
			return
		}
	}
}

// ownerNames are the method parameters, the fields and methods of a type that are called by name.
func (c *completer) ownerNames(owner *types.Decl, member ast.Member) {
	var params []*types.Var
	switch m := member.(type) {
	case *ast.OnDecl:
		if ev := owner.Method(m.Event.Name); ev != nil {
			params = ev.Params
		}
	default:
		if id := memberIdent(member); id != nil {
			if md := owner.Method(id.Name); md != nil && md.Node == member {
				params = md.Params
			}
		}
	}
	for _, p := range params {
		c.add(CompletionItem{Label: p.Name, Kind: kindVariable, Detail: typeString(p.Type), SortText: "2" + p.Name})
	}
	if owner.ID != nil {
		c.add(CompletionItem{Label: "id", Kind: kindField, Detail: owner.ID.String(), SortText: "3id"})
	}
	for _, f := range owner.Fields {
		c.add(CompletionItem{Label: f.Name, Kind: kindField, Detail: typeString(f.Type), SortText: "3" + f.Name})
	}
	for _, md := range owner.Methods {
		switch md.Kind {
		case types.Command, types.Event, types.Error, types.Notification, types.Rule, types.ValueMethod:
			c.method(owner, md)
		}
	}
}

// typeNames are the type names of the model.
func (c *completer) typeNames() {
	names := make([]string, 0, len(c.r.info.Decls))
	for name := range c.r.info.Decls {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		d := c.r.info.Lookup(name)
		kind := kindClass
		switch d.Kind {
		case types.ValueDecl:
			kind = kindStruct
		case types.EnumDecl:
			kind = kindEnum
		case types.ExternalDecl:
			kind = kindInterface
		}
		c.add(CompletionItem{Label: name, Kind: kind, Detail: d.Kind.String(), SortText: "5" + name,
			Documentation: c.doc(docOf(d.Node))})
	}
}

func (c *completer) doc(g *ast.CommentGroup) *markupContent {
	if text := g.Text(); text != "" {
		return &markupContent{Kind: "markdown", Value: text}
	}
	return nil
}
