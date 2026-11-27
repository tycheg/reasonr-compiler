package parser

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

// UI (RFC §22): apps, screens, layout, primitives, default, texts.

func (p *parser) parseApp(doc *ast.CommentGroup) *ast.AppDecl {
	d := &ast.AppDecl{Doc: doc, App: p.tok.Pos}
	p.next()
	if d.Access = p.parseAccess(); d.Access == nil {
		p.errorExpected(quote("("))
	}
	d.Name = p.parseIdent()
	if p.at(token.STRING) {
		d.Title = p.parseStringLit()
	}
	if p.atWord("auto") {
		d.AutoPos = p.tok.Pos
		p.next()
	}
	p.expectNewline()
	if p.at(token.INDENT) || !d.IsAuto() {
		if p.openBody(headerRange(d.App, d.Name), whatApp) {
			p.layout = true
			p.blockLoop(func() { d.Items = append(d.Items, p.parseAppItem()) })
			p.layout = false
			p.expectDedent()
		}
	}
	d.EndPos = p.prevEnd
	return d
}

func (p *parser) parseAppItem() ast.AppItem {
	switch {
	case p.atWord("language") && p.peek(1).Kind == token.IDENT:
		it := &ast.LanguageItem{Language: p.tok.Pos}
		p.next()
		it.Name = p.parseIdent()
		p.expectNewline()
		return it
	case p.atWord("platforms") && p.peek(1).Kind == token.IDENT:
		it := &ast.PlatformsItem{Platforms: p.tok.Pos}
		p.next()
		for {
			it.Names = append(it.Names, p.parseIdent())
			if !p.at(token.COMMA) {
				break
			}
			p.next()
		}
		p.expectNewline()
		return it
	case p.atDefault():
		return p.parseDefault(p.doc(p.tok.Pos))
	}
	return p.parseNode().(ast.AppItem) // every layout node is also an app line
}

// atDefault: default Type or default with a constraint (enum, value).
func (p *parser) atDefault() bool {
	if !p.atWord("default") {
		return false
	}
	switch p.peek(1).Kind {
	case token.IDENT, token.ENUM, token.VALUE:
		return true
	}
	return false
}

func (p *parser) parseDefault(doc *ast.CommentGroup) *ast.DefaultDecl {
	d := &ast.DefaultDecl{Doc: doc, Default: p.tok.Pos}
	p.next()
	p.uiTypes = true
	d.Type = p.parseType()
	p.uiTypes = false
	d.Prim = p.parseIdent()
	if p.at(token.LPAREN) {
		d.Args = p.parseArgs()
	}
	p.expectNewline()
	return d
}

func (p *parser) parseScreen(doc *ast.CommentGroup) *ast.ScreenDecl {
	d := &ast.ScreenDecl{Doc: doc, Screen: p.tok.Pos}
	p.next()
	d.Name = p.parseIdent()
	if p.at(token.LPAREN) {
		d.Params = p.parseParams()
	}
	if p.at(token.STRING) {
		d.Title = p.parseStringLit()
	}
	p.expectNewline()
	if p.openBody(headerRange(d.Screen, d.Name), whatScreen) {
		p.layout = true
		d.Nodes = p.parseNodeList()
		p.layout = false
		p.expectDedent()
	}
	d.EndPos = p.prevEnd
	return d
}

// parseNodeList reads layout lines to the end of the block.
func (p *parser) parseNodeList() []ast.UINode {
	var list []ast.UINode
	p.blockLoop(func() { list = append(list, p.parseNode()) })
	return list
}

// parseNode parses a layout line: a primitive, a part or a screen (capital
// letter), a field (lowercase) or an on transition.
func (p *parser) parseNode() ast.UINode {
	switch {
	case p.atWord("on") && p.peek(1).Kind == token.IDENT:
		return p.parseOn()
	case p.at(token.IDENT) && isUpper(p.tok.Text):
		return p.parsePrimNode()
	case p.at(token.IDENT):
		return p.parseFieldNode()
	}
	from := p.tok.Pos
	p.errorExpected(wantNode)
	p.syncLine()
	return &ast.BadNode{From: from, To: p.prevEnd}
}

func (p *parser) parsePrimNode() *ast.PrimNode {
	n := &ast.PrimNode{Name: p.parseIdent()}
	if p.at(token.LPAREN) {
		n.Args = p.parseArgs()
	}
	if p.at(token.LBRACK) || (p.at(token.IDENT) && isUpper(p.tok.Text)) {
		n.Binding = p.parseBinding()
	}
	n.Label, n.IfPos, n.Cond = p.parseNodeTail()
	p.expectNewline()
	n.Children = p.parseNodeChildren()
	n.EndPos = p.prevEnd
	return n
}

// parseBinding parses a binding: Type.Method, [target].Command or Screen,
// with arguments in parentheses.
func (p *parser) parseBinding() *ast.Binding {
	b := &ast.Binding{}
	if p.at(token.LBRACK) {
		x := &ast.BracketExpr{Lbrack: p.tok.Pos}
		p.next()
		x.Elems = []ast.Expr{p.parseExpr()}
		x.Rbrack = p.expectClose(token.RBRACK)
		b.Target = x
		if !p.at(token.DOT) {
			p.errorExpected(wantMethod)
		} else {
			b.Dot = p.tok.Pos
			p.next()
		}
		b.Method = p.parseIdent()
	} else {
		b.Type = p.parseIdent()
		if p.at(token.DOT) {
			b.Dot = p.tok.Pos
			p.next()
			b.Method = p.parseIdent()
		}
	}
	if p.at(token.LPAREN) {
		b.Args = p.parseArgs()
	}
	return b
}

func (p *parser) parseFieldNode() *ast.FieldNode {
	n := &ast.FieldNode{Name: p.parseIdent()}
	if p.at(token.IDENT) && isUpper(p.tok.Text) {
		n.Prim = p.parseIdent()
		if p.at(token.LPAREN) {
			n.PrimArgs = p.parseArgs()
		}
	}
	n.Label, n.IfPos, n.Cond = p.parseNodeTail()
	p.expectNewline()
	n.Children = p.parseNodeChildren()
	n.EndPos = p.prevEnd
	return n
}

// parseNodeTail reads the end of a layout line: the label and the visibility condition.
func (p *parser) parseNodeTail() (label *ast.BasicLit, ifPos source.Pos, cond ast.Expr) {
	if p.at(token.STRING) {
		label = p.parseStringLit()
	}
	if p.at(token.IF) {
		ifPos = p.tok.Pos
		p.next()
		cond = p.parseExpr()
	}
	return label, ifPos, cond
}

func (p *parser) parseNodeChildren() []ast.UINode {
	if !p.at(token.INDENT) {
		return nil
	}
	p.next()
	list := p.parseNodeList()
	p.expectDedent()
	return list
}

func (p *parser) parseOn() *ast.OnNode {
	n := &ast.OnNode{On: p.tok.Pos}
	p.next()
	n.Event = p.parseIdent()
	switch {
	case p.atWord("back"):
		n.BackPos = p.tok.Pos
		p.next()
	case p.at(token.IDENT):
		n.Screen = p.parseIdent()
		if p.at(token.LPAREN) {
			n.Args = p.parseArgs()
		}
	default:
		p.errorExpected(wantScreen)
	}
	p.expectNewline()
	return n
}

// parseExtendScreen reads the in Part blocks of a screen extension (RFC §21).
func (p *parser) parseExtendScreen(header source.Range) []*ast.ExtendIn {
	p.expectNewline()
	if !p.openBody(header, whatExtend) {
		return nil
	}
	var ins []*ast.ExtendIn
	p.layout = true
	p.blockLoop(func() {
		if !p.at(token.IN) {
			p.errorExpected(quote("in"))
			p.syncLine()
			return
		}
		in := &ast.ExtendIn{In: p.tok.Pos}
		p.next()
		in.Part = p.parseIdent()
		p.expectNewline()
		if p.openBody(headerRange(in.In, in.Part), whatIn) {
			in.Nodes = p.parseNodeList()
			p.expectDedent()
		}
		in.EndPos = p.prevEnd
		ins = append(ins, in)
	})
	p.layout = false
	p.expectDedent()
	return ins
}

// ---------------------------------------------------------------------------
// Primitives

func (p *parser) parsePrimitive(doc *ast.CommentGroup) *ast.PrimitiveDecl {
	d := &ast.PrimitiveDecl{Doc: doc, Primitive: p.tok.Pos}
	p.next()
	d.Name = p.parseIdent()
	p.uiTypes = true
	defer func() { p.uiTypes = false }()
	if p.at(token.LT) {
		d.TypeParams = p.parseTypeParams()
	}
	if p.at(token.LPAREN) {
		d.Params = p.parsePrimParams()
	}
	p.expectNewline()
	if p.at(token.INDENT) {
		p.next()
		p.blockLoop(func() { d.Items = append(d.Items, p.parsePrimItem()) })
		p.expectDedent()
	}
	d.EndPos = p.prevEnd
	return d
}

func (p *parser) parseTypeParams() *ast.TypeParamList {
	l := &ast.TypeParamList{Lt: p.tok.Pos}
	p.next()
	for !p.at(token.GT) && !p.atLineEnd() {
		tp := &ast.TypeParam{Name: p.parseIdent()}
		if p.atWord("id") || p.at(token.ENUM) || p.at(token.VALUE) {
			tp.Constraint = &ast.Ident{NamePos: p.tok.Pos, Name: p.tok.Text}
			p.next()
		}
		l.List = append(l.List, tp)
		if !p.at(token.COMMA) {
			break
		}
		p.next()
	}
	l.Gt = p.expectClose(token.GT)
	return l
}

func (p *parser) parsePrimParams() *ast.PrimParamList {
	l := &ast.PrimParamList{Lparen: p.tok.Pos}
	p.next()
	for !p.at(token.RPAREN) && !p.atLineEnd() {
		pp := &ast.PrimParam{Name: p.parseIdent()}
		if p.at(token.LPAREN) {
			pp.Query = p.parseParams()
		}
		pp.Type = p.parseTypeUnion()
		if p.at(token.ASSIGN) {
			p.next()
			if p.atWord("auto") {
				pp.AutoPos = p.tok.Pos
				p.next()
			} else {
				pp.Default = p.parseExpr()
			}
		}
		l.List = append(l.List, pp)
		if !p.at(token.COMMA) {
			break
		}
		p.next()
	}
	l.Rparen = p.expectClose(token.RPAREN)
	return l
}

// parseTypeUnion reads a type or variants A | B.
func (p *parser) parseTypeUnion() ast.TypeExpr {
	t := p.parseType()
	if !p.at(token.PIPE) {
		return t
	}
	u := &ast.UnionType{Types: []ast.TypeExpr{t}}
	for p.at(token.PIPE) {
		p.next()
		u.Types = append(u.Types, p.parseType())
	}
	return u
}

func (p *parser) parsePrimItem() ast.PrimItem {
	switch {
	case p.atWord("binds"):
		it := &ast.BindsItem{Binds: p.tok.Pos}
		p.next()
		switch {
		case p.at(token.COMMAND), p.at(token.SCREEN):
			it.KindPos, it.Kind = p.tok.Pos, p.tok.Text
			p.next()
		case (p.atWord(ast.BindField) || p.atWord(ast.BindInput)) && startsType(p.peek(1).Kind):
			it.KindPos, it.Kind = p.tok.Pos, p.tok.Text
			p.next()
			it.Type = p.parseTypeUnion()
		case startsType(p.tok.Kind):
			it.Type = p.parseTypeUnion()
		default:
			p.errorExpected(wantBindKind)
		}
		p.expectNewline()
		return it
	case p.atWord("children") && isWord(p.peek(1), "binds"):
		it := &ast.PartItem{Part: p.tok.Pos, Children: true}
		p.next()
		p.parsePartBinds(it)
		p.expectNewline()
		return it
	case p.atWord("part"):
		it := &ast.PartItem{Part: p.tok.Pos}
		p.next()
		it.Name = p.parseIdent()
		if p.atWord("binds") {
			p.parsePartBinds(it)
		}
		p.expectNewline()
		return it
	case p.at(token.EVENT):
		it := &ast.PrimEvent{Event: p.tok.Pos}
		p.next()
		it.Name = p.parseIdent()
		if p.at(token.LPAREN) {
			it.Params = p.parseParams()
		}
		p.expectNewline()
		return it
	}
	from := p.tok.Pos
	p.errorExpected(wantPrimItem)
	p.syncLine()
	return &ast.BadNode{From: from, To: p.prevEnd}
}

// parsePartBinds reads "binds what" of a part; the current token is binds.
func (p *parser) parsePartBinds(it *ast.PartItem) {
	it.BindsPos = p.tok.Pos
	p.next()
	switch {
	case p.atWord(ast.PartParams), p.atWord(ast.PartInputs), p.atWord(ast.PartScreens):
		it.KindPos, it.Kind = p.tok.Pos, p.tok.Text
		p.next()
	case startsType(p.tok.Kind):
		it.Type = p.parseType()
	default:
		p.errorExpected(wantPartKind)
	}
}

func startsType(k token.Kind) bool {
	return k == token.IDENT || k == token.ENUM || k == token.VALUE
}

// ---------------------------------------------------------------------------
// Texts

func (p *parser) parseTexts(doc *ast.CommentGroup) *ast.TextsDecl {
	d := &ast.TextsDecl{Doc: doc, Texts: p.tok.Pos}
	p.next()
	d.Lang = p.parseIdent()
	p.expectNewline()
	if p.openBody(headerRange(d.Texts, d.Lang), whatTexts) {
		p.blockLoop(func() {
			if !p.at(token.IDENT) {
				p.errorExpected(wantTextKey)
				p.syncLine()
				return
			}
			e := &ast.TextEntry{Key: p.parseTextKey()}
			if p.at(token.STRING) {
				e.Value = p.parseString()
			} else {
				p.errorExpected(wantString)
				e.Value = &ast.BasicLit{ValuePos: p.missing(), Kind: token.STRING}
			}
			d.Entries = append(d.Entries, e)
			p.expectNewline()
		})
		p.expectDedent()
	}
	d.EndPos = p.prevEnd
	return d
}

func (p *parser) parseTextKey() *ast.TextKey {
	k := &ast.TextKey{Path: p.parsePath()}
	if p.at(token.COLON) {
		k.Colon = p.tok.Pos
		p.next()
		k.Node = p.parsePath()
	}
	if p.at(token.HASH) {
		k.Hash = p.tok.Pos
		p.next()
		if p.at(token.INT) {
			k.N = p.basicLit()
		} else {
			p.errorExpected("a node number")
		}
	}
	return k
}
