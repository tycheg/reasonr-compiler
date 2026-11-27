package parser

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

func (p *parser) parseFile() *ast.File {
	f := &ast.File{
		Name:      p.file.Name(),
		FileStart: p.file.Pos(0),
		FileEnd:   p.file.Pos(p.file.Size()),
	}
	for !p.at(token.EOF) {
		switch {
		case p.at(token.INDENT):
			p.unexpectedIndent()
			continue
		case p.at(token.DEDENT), p.at(token.NEWLINE):
			p.next()
			continue
		}
		before := p.i
		f.Decls = append(f.Decls, p.parseDecl())
		if p.i == before {
			p.syncLine()
		}
	}
	return f
}

func (p *parser) parseDecl() ast.Decl {
	doc := p.doc(p.tok.Pos)
	switch p.tok.Kind {
	case token.ROLE:
		d := &ast.RoleDecl{Doc: doc, Role: p.tok.Pos}
		p.next()
		d.Name = p.parseIdent()
		p.expectNewline()
		return d
	case token.ENUM:
		return p.parseEnum(doc)
	case token.VALUE:
		return p.parseValue(doc)
	case token.AGGREGATE, token.ENTITY, token.ACTOR:
		d := &ast.EntityDecl{Doc: doc, Keyword: p.tok.Pos, IsActor: p.at(token.ACTOR), IsAggregate: p.at(token.AGGREGATE)}
		entity := p.at(token.ENTITY)
		p.next()
		d.Name = p.parseIdent()
		if entity && p.at(token.DOT) {
			p.next()
			d.Owner, d.Name = d.Name, p.parseIdent()
		}
		d.Members = p.parseMemberBlock(d.Pos(), d.Name, whatEntity, p.parseEntityMember)
		d.EndPos = p.prevEnd
		return d
	case token.PROJECTION:
		d := &ast.ProjectionDecl{Doc: doc, Projection: p.tok.Pos}
		p.next()
		d.Name = p.parseIdent()
		d.Members = p.parseMemberBlock(d.Pos(), d.Name, whatProjection, p.parseEntityMember)
		d.EndPos = p.prevEnd
		return d
	case token.EXTERNAL:
		d := &ast.ExternalDecl{Doc: doc, External: p.tok.Pos}
		p.next()
		d.Access = p.parseAccess()
		d.Name = p.parseIdent()
		d.Members = p.parseMemberBlock(d.Pos(), d.Name, whatExternal, p.parseExternalMember)
		d.EndPos = p.prevEnd
		return d
	case token.APP:
		return p.parseApp(doc)
	case token.SCREEN:
		return p.parseScreen(doc)
	case token.PRIMITIVE:
		return p.parsePrimitive(doc)
	case token.TEXTS:
		return p.parseTexts(doc)
	case token.MIGRATION:
		d := &ast.MigrationDecl{Doc: doc, Migration: p.tok.Pos}
		p.next()
		d.Name = p.parseIdent()
		p.expectNewline()
		d.Body = p.parseBlock(headerRange(d.Migration, d.Name), whatMigration)
		return d
	case token.TEST:
		return p.parseTest(doc)
	case token.EXTEND:
		return p.parseExtend(doc)
	case token.LET:
		return &ast.LetDecl{Doc: doc, Let: p.parseLet()}
	}
	if p.atDefault() {
		return p.parseDefault(doc)
	}
	if p.atWord("feature") && p.peek(1).Kind == token.IDENT {
		d := &ast.FeatureDecl{Doc: doc, Feature: p.tok.Pos}
		p.next()
		d.Name = p.parseIdent()
		d.Date = p.parseStringLit()
		p.expectNewline()
		return d
	}
	if p.atWord("acl") && p.peek(1).Kind == token.IDENT {
		d := &ast.ACLDecl{Doc: doc, ACL: p.tok.Pos}
		p.next()
		d.Source = p.parseIdent()
		d.Members = p.parseMemberBlock(d.Pos(), d.Source, whatACL, p.parseACLMember)
		d.EndPos = p.prevEnd
		return d
	}
	if p.atWord("exclude") && p.peek(1).Kind == token.TEST {
		return p.parseExclude()
	}
	if p.atWord("fake") && p.peek(1).Kind == token.IDENT {
		d := &ast.FakeDecl{Doc: doc, Fake: p.tok.Pos}
		p.next()
		d.Name = p.parseIdent()
		d.Members = p.parseMemberBlock(d.Pos(), d.Name, whatFake, p.parseFakeMember)
		d.EndPos = p.prevEnd
		return d
	}
	from := p.tok.Pos
	p.errorExpected(wantDecl)
	p.syncLine()
	return &ast.BadDecl{From: from, To: p.prevEnd}
}

func headerRange(start source.Pos, name *ast.Ident) source.Range {
	return source.Range{Start: start, End: name.End()}
}

func (p *parser) parseEnum(doc *ast.CommentGroup) *ast.EnumDecl {
	d := &ast.EnumDecl{Doc: doc, Enum: p.tok.Pos}
	p.next()
	d.Name = p.parseIdent()
	p.expectNewline()
	if p.openBody(headerRange(d.Enum, d.Name), whatEnum) {
		p.blockLoop(func() {
			v := &ast.EnumValue{Doc: p.doc(p.tok.Pos), Name: p.parseIdent()}
			v.Was = p.parseWas()
			d.Values = append(d.Values, v)
			p.expectNewline()
		})
		p.expectDedent()
	}
	d.EndPos = p.prevEnd
	return d
}

func (p *parser) parseValue(doc *ast.CommentGroup) *ast.ValueDecl {
	d := &ast.ValueDecl{Doc: doc, Value: p.tok.Pos}
	p.next()
	d.Name = p.parseIdent()
	d.Members = p.parseMemberBlock(d.Value, d.Name, whatValue, p.parseValueMember)
	d.EndPos = p.prevEnd
	return d
}

// parseMemberBlock reads the end of a heading and the block of members.
func (p *parser) parseMemberBlock(start source.Pos, name *ast.Ident, what string, member func() ast.Member) []ast.Member {
	p.expectNewline()
	if !p.openBody(headerRange(start, name), what) {
		return nil
	}
	var list []ast.Member
	p.blockLoop(func() {
		if m := member(); m != nil {
			list = append(list, m)
		}
	})
	p.expectDedent()
	return list
}

func (p *parser) badMember(from source.Pos, what string) ast.Member {
	p.errorExpected(what)
	p.syncLine()
	return &ast.BadMember{From: from, To: p.prevEnd}
}

// parseEntityMember parses a member of an entity, actor, projection or extension.
func (p *parser) parseEntityMember() ast.Member {
	doc := p.doc(p.tok.Pos)
	switch p.tok.Kind {
	case token.INDEX:
		return p.parseIndex(doc)
	case token.COMMAND:
		return p.parseCommand(doc)
	case token.EVENT:
		return p.parseEvent(doc)
	case token.NOTIFICATION:
		m := &ast.NotificationDecl{Doc: doc, Notification: p.tok.Pos}
		p.next()
		m.Name = p.parseIdent()
		m.Params = p.parseParams()
		p.expectNewline()
		return m
	case token.ERROR:
		m := &ast.ErrorDecl{Doc: doc, Error: p.tok.Pos}
		p.next()
		m.Name = p.parseIdent()
		if p.at(token.LPAREN) {
			m.Params = p.parseParams()
		}
		p.expectNewline()
		return m
	case token.QUERY:
		return p.parseQuery(doc)
	case token.RULE:
		m := &ast.RuleDecl{Doc: doc, Rule: p.tok.Pos}
		p.next()
		m.Name = p.parseIdent()
		m.Params = p.parseParams()
		m.Result = p.parseType()
		p.expectNewline()
		m.Body = p.parseBlock(headerRange(m.Rule, m.Name), whatRule)
		return m
	case token.IDENT:
		switch {
		case p.atWord("id") && p.peek(1).Kind == token.IDENT:
			m := &ast.IDDecl{Doc: doc, ID: p.tok.Pos}
			p.next()
			m.Type = p.parseType()
			p.expectNewline()
			return m
		case p.atWord("on") && p.onHeader():
			return p.parseOnDecl(doc)
		case p.atWord("check"):
			return p.parseCheck(doc)
		case p.peek(1).Kind == token.LPAREN:
			return p.parseMethod(doc)
		}
		return p.parseField(doc)
	}
	return p.badMember(p.tok.Pos, wantMember)
}

// parseOnDecl parses a reaction: on event or on Context.Entity.event.
func (p *parser) parseOnDecl(doc *ast.CommentGroup) *ast.OnDecl {
	m := &ast.OnDecl{Doc: doc, On: p.tok.Pos}
	p.next()
	m.Event = p.parseIdent()
	for p.at(token.DOT) {
		p.next()
		m.Path = append(m.Path, m.Event)
		m.Event = p.parseIdent()
	}
	p.expectNewline()
	m.Body = p.parseBlock(headerRange(m.On, m.Event), whatOn)
	return m
}

// parseACLMember parses an ACL member: an on reaction to a source event or
// actor Source.Actor as Actor.
func (p *parser) parseACLMember() ast.Member {
	doc := p.doc(p.tok.Pos)
	switch {
	case p.atWord("on") && p.onHeader():
		return p.parseOnDecl(doc)
	case p.at(token.ACTOR):
		m := &ast.ActorAsDecl{Doc: doc, Actor: p.tok.Pos}
		p.next()
		m.From = append(m.From, p.parseIdent())
		for p.at(token.DOT) {
			p.next()
			m.From = append(m.From, p.parseIdent())
		}
		if !p.atWord("as") {
			p.errorExpected(quote("as"))
			p.syncLine()
			return &ast.BadMember{From: m.Actor, To: p.prevEnd}
		}
		p.next()
		m.To = p.parseIdent()
		p.expectNewline()
		return m
	}
	return p.badMember(p.tok.Pos, wantACLMember)
}

// parseValueMember parses a value object member.
func (p *parser) parseValueMember() ast.Member {
	doc := p.doc(p.tok.Pos)
	if p.at(token.IDENT) {
		switch {
		case p.atWord("check"):
			return p.parseCheck(doc)
		case p.peek(1).Kind == token.LPAREN:
			return p.parseMethod(doc)
		}
		return p.parseField(doc)
	}
	return p.badMember(p.tok.Pos, wantValueMember)
}

func (p *parser) parseExternalMember() ast.Member {
	if !p.at(token.REQUEST) {
		return p.badMember(p.tok.Pos, wantRequest)
	}
	return p.parseRequest(false)
}

// parseFakeMember parses a fake member: a field, an event, a command or a
// request implementation.
func (p *parser) parseFakeMember() ast.Member {
	doc := p.doc(p.tok.Pos)
	switch p.tok.Kind {
	case token.COMMAND:
		return p.parseCommand(doc)
	case token.EVENT:
		return p.parseEvent(doc)
	case token.REQUEST:
		return p.parseRequest(true)
	case token.IDENT:
		if p.peek(1).Kind != token.LPAREN {
			return p.parseField(doc)
		}
	}
	return p.badMember(p.tok.Pos, wantFakeMember)
}

// parseRequest parses a request; body is an implementation with a body in a fake.
func (p *parser) parseRequest(body bool) *ast.RequestDecl {
	m := &ast.RequestDecl{Doc: p.doc(p.tok.Pos), Request: p.tok.Pos}
	p.next()
	m.Name = p.parseIdent()
	m.Params = p.parseParams()
	if p.atWord("timeout") {
		m.TimeoutPos = p.tok.Pos
		p.next()
		m.Timeout = p.parseExpr()
	}
	p.expectNewline()
	if body {
		m.Body = p.parseBlock(headerRange(m.Request, m.Name), whatRequest)
	}
	return m
}

func (p *parser) parseField(doc *ast.CommentGroup) *ast.FieldDecl {
	m := &ast.FieldDecl{Doc: doc, Name: p.parseIdent()}
	m.Type = p.parseType()
	if p.at(token.ASSIGN) {
		p.next()
		m.Default = p.parseExpr()
	}
	if p.atWord("personal") {
		m.Personal = p.parsePersonal()
	}
	if p.atWord("was") {
		m.WasPos = p.tok.Pos
		p.next()
		m.Was = p.parseIdent()
	}
	p.expectNewline()
	return m
}

func (p *parser) parseCheck(doc *ast.CommentGroup) *ast.CheckDecl {
	m := &ast.CheckDecl{Doc: doc, Check: p.tok.Pos}
	p.next()
	m.Cond = p.parseExpr()
	p.expectNewline()
	return m
}

func (p *parser) parseMethod(doc *ast.CommentGroup) *ast.MethodDecl {
	m := &ast.MethodDecl{Doc: doc, Name: p.parseIdent()}
	m.Params = p.parseParams()
	m.Result = p.parseType()
	p.expectNewline()
	m.Body = p.parseBlock(headerRange(m.Name.Pos(), m.Name), whatMethod)
	return m
}

func (p *parser) parseIndex(doc *ast.CommentGroup) *ast.IndexDecl {
	m := &ast.IndexDecl{Doc: doc, Index: p.tok.Pos}
	p.next()
	m.Name = p.parseIdent()
	m.Lparen = p.expect(token.LPAREN)
	if !p.at(token.RPAREN) && !p.atLineEnd() {
		m.Keys = p.parseSortKeys()
	}
	m.Rparen = p.expectClose(token.RPAREN)
	if p.atWord("unique") {
		m.UniquePos = p.tok.Pos
		p.next()
		m.Error = p.parseIdent()
	}
	p.expectNewline()
	return m
}

func (p *parser) parseCommand(doc *ast.CommentGroup) *ast.CommandDecl {
	m := &ast.CommandDecl{Doc: doc, Command: p.tok.Pos}
	p.next()
	m.Access = p.parseAccess()
	if p.atWord("create") && p.peek(1).Kind == token.IDENT {
		m.Create = p.tok.Pos
		p.next()
	}
	m.Name = p.parseIdent()
	m.Params = p.parseParams()
	m.Was = p.parseWas()
	p.expectNewline()
	m.Body = p.parseBlock(headerRange(m.Command, m.Name), whatCommand)
	return m
}

func (p *parser) parseEvent(doc *ast.CommentGroup) *ast.EventDecl {
	m := &ast.EventDecl{Doc: doc, Event: p.tok.Pos}
	p.next()
	m.Name = p.parseIdent()
	m.Params = p.parseParams()
	p.expectNewline()
	if p.at(token.INDENT) {
		m.Body = p.parseBlock(headerRange(m.Event, m.Name), whatEvent)
	}
	return m
}

func (p *parser) parseQuery(doc *ast.CommentGroup) *ast.QueryDecl {
	m := &ast.QueryDecl{Doc: doc, Query: p.tok.Pos}
	p.next()
	m.Access = p.parseAccess()
	m.Name = p.parseIdent()
	m.Params = p.parseParams()
	m.Result = p.parseType()
	m.Was = p.parseWas()
	p.expectNewline()
	m.Body = p.parseBlock(headerRange(m.Query, m.Name), whatQuery)
	return m
}

// ---------------------------------------------------------------------------
// Extensions

func (p *parser) parseExtend(doc *ast.CommentGroup) *ast.ExtendDecl {
	d := &ast.ExtendDecl{Doc: doc, Extend: p.tok.Pos}
	p.next()
	if p.at(token.SCREEN) {
		d.ScreenPos = p.tok.Pos
		p.next()
		d.Name = p.parseIdent()
		d.Ins = p.parseExtendScreen(headerRange(d.Extend, d.Name))
		d.EndPos = p.prevEnd
		return d
	}
	d.Name = p.parseIdent()
	d.Members = p.parseMemberBlock(d.Extend, d.Name, whatExtend, p.parseEntityMember)
	d.EndPos = p.prevEnd
	return d
}

func (p *parser) parseExclude() *ast.ExcludeDecl {
	d := &ast.ExcludeDecl{Exclude: p.tok.Pos}
	p.next()
	d.TestPos = p.tok.Pos
	p.next() // test
	d.Test = p.parseStringLit()
	d.Because = p.tok.Pos
	if p.atWord("because") {
		p.next()
	} else {
		p.errorExpected(quote("because"))
	}
	d.Reason = p.parseStringLit()
	p.expectNewline()
	return d
}

// parseStringLit reads a string without interpolation (a test name, a reason).
func (p *parser) parseStringLit() *ast.BasicLit {
	if !p.at(token.STRING) {
		p.errorExpected(wantString)
		return &ast.BasicLit{ValuePos: p.missing(), Kind: token.STRING}
	}
	x := p.parseOperand()
	if lit, ok := x.(*ast.BasicLit); ok {
		return lit
	}
	p.error(source.Range{Start: x.Pos(), End: x.End()}, diag.E0008, "interpolation is not allowed here")
	return &ast.BasicLit{ValuePos: x.Pos(), Kind: token.STRING, Raw: string(p.file.Source()[p.file.Offset(x.Pos()):p.file.Offset(x.End())])}
}

// onHeader reports whether the current on starts a reaction heading:
// on name{.name} NEWLINE INDENT.
func (p *parser) onHeader() bool {
	i := 1
	for {
		if p.peek(i).Kind != token.IDENT {
			return false
		}
		if p.peek(i+1).Kind != token.DOT {
			break
		}
		i += 2
	}
	return p.peek(i+1).Kind == token.NEWLINE && p.peek(i+2).Kind == token.INDENT
}
