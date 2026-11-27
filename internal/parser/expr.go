package parser

import (
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

// Operator precedence, RFC §8.1, ascending:
// or < and < comparisons < ?? < + - < * / % < unary < postfix.

func (p *parser) parseExpr() ast.Expr { return p.parseOr() }

func (p *parser) parseOr() ast.Expr {
	x := p.parseAnd()
	for p.at(token.OR) && !p.lineDone {
		op := p.tok
		p.next()
		x = &ast.BinaryExpr{X: x, OpPos: op.Pos, Op: op.Kind, Y: p.parseAnd()}
	}
	return x
}

func (p *parser) parseAnd() ast.Expr {
	x := p.parseCompare()
	for p.at(token.AND) && !p.lineDone {
		op := p.tok
		p.next()
		x = &ast.BinaryExpr{X: x, OpPos: op.Pos, Op: op.Kind, Y: p.parseCompare()}
	}
	return x
}

func isComparison(k token.Kind) bool {
	switch k {
	case token.EQ, token.NE, token.LT, token.LE, token.GT, token.GE:
		return true
	}
	return false
}

// parseCompare parses comparisons. They are not associative: a < b < c is an error.
func (p *parser) parseCompare() ast.Expr {
	x := p.parseCoalesce()
	for n := 0; !p.lineDone; n++ {
		start := p.tok
		switch {
		case isComparison(p.tok.Kind):
			p.next()
			x = &ast.BinaryExpr{X: x, OpPos: start.Pos, Op: start.Kind, Y: p.parseCoalesce()}
		case p.at(token.IN):
			p.next()
			x = &ast.InExpr{X: x, OpPos: start.Pos, Y: p.parseCoalesce()}
		case p.at(token.NOT) && p.peek(1).Kind == token.IN:
			p.next()
			p.next()
			x = &ast.InExpr{X: x, OpPos: start.Pos, Not: true, Y: p.parseCoalesce()}
		case p.at(token.IS):
			p.next()
			e := &ast.IsNoneExpr{X: x, Is: start.Pos}
			if p.at(token.NOT) {
				e.Not = true
				p.next()
			}
			e.EndPos = p.tok.End
			if p.at(token.NONE) {
				p.next()
			} else {
				p.errorExpected(quote("none"))
				e.EndPos = p.prevEnd
			}
			x = e
		default:
			return x
		}
		if n > 0 {
			// The chain becomes a BadExpr: it has no meaning, and type
			// checking must not report its own errors on it.
			p.error(source.Range{Start: x.Pos(), End: x.End()}, diag.E0017)
			x = &ast.BadExpr{From: x.Pos(), To: x.End()}
		}
	}
	return x
}

// parseCoalesce parses "??"; the operator is right-associative.
func (p *parser) parseCoalesce() ast.Expr {
	x := p.parseAdditive()
	if p.at(token.COALESCE) && !p.lineDone {
		op := p.tok
		p.next()
		return &ast.BinaryExpr{X: x, OpPos: op.Pos, Op: op.Kind, Y: p.parseCoalesce()}
	}
	return x
}

func (p *parser) parseAdditive() ast.Expr {
	x := p.parseMultiplicative()
	for (p.at(token.ADD) || p.at(token.SUB)) && !p.lineDone {
		op := p.tok
		p.next()
		x = &ast.BinaryExpr{X: x, OpPos: op.Pos, Op: op.Kind, Y: p.parseMultiplicative()}
	}
	return x
}

func (p *parser) parseMultiplicative() ast.Expr {
	x := p.parseUnary()
	for (p.at(token.MUL) || p.at(token.QUO) || p.at(token.REM)) && !p.lineDone {
		op := p.tok
		p.next()
		x = &ast.BinaryExpr{X: x, OpPos: op.Pos, Op: op.Kind, Y: p.parseUnary()}
	}
	return x
}

func (p *parser) parseUnary() ast.Expr {
	if p.at(token.SUB) || p.at(token.NOT) {
		op := p.tok
		p.next()
		return &ast.UnaryExpr{OpPos: op.Pos, Op: op.Kind, X: p.parseUnary()}
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() ast.Expr {
	x := p.parseOperand()
	for !p.lineDone {
		switch p.tok.Kind {
		case token.DOT, token.SAFEDOT:
			sel := &ast.SelectorExpr{X: x, Dot: p.tok.Pos, Safe: p.at(token.SAFEDOT)}
			p.next()
			sel.Sel = p.parseIdent()
			x = sel
		case token.LPAREN:
			x = &ast.CallExpr{Fun: x, Args: p.parseArgs()}
		default:
			return x
		}
	}
	return x
}

func (p *parser) basicLit() *ast.BasicLit {
	lit := &ast.BasicLit{ValuePos: p.tok.Pos, Kind: p.tok.Kind, Raw: p.tok.Text, Value: p.tok.Text}
	p.next()
	return lit
}

func (p *parser) parseOperand() ast.Expr {
	switch p.tok.Kind {
	case token.IDENT:
		if p.atWord("all") && p.peek(1).Kind == token.IDENT && isUpper(p.peek(1).Text) {
			x := &ast.AllExpr{All: p.tok.Pos}
			p.next()
			x.Type = p.parseIdent()
			return x
		}
		return p.parseIdent()
	case token.INT, token.DECIMAL, token.DURATION, token.TRUE, token.FALSE, token.NONE:
		return p.basicLit()
	case token.DATE, token.TIMESTAMP:
		lit := p.basicLit()
		if i := strings.IndexByte(lit.Raw, '"'); i >= 0 {
			lit.Value = strings.TrimSuffix(lit.Raw[i+1:], `"`)
		}
		return lit
	case token.STRING:
		return p.parseString()
	case token.NEW:
		return p.parseNewID()
	case token.LPAREN:
		x := &ast.ParenExpr{Lparen: p.tok.Pos}
		p.next()
		x.X = p.parseExpr()
		x.Rparen = p.expectClose(token.RPAREN)
		return x
	case token.LBRACK:
		return p.parseBracket()
	}
	from := p.tok.Pos
	p.errorExpected(wantExpr)
	switch p.tok.Kind {
	case token.RPAREN, token.RBRACK, token.COMMA, token.COLON, token.ASSIGN,
		token.NEWLINE, token.INDENT, token.DEDENT, token.EOF:
		// Closing tokens and line ends are left to the outer rules.
		return &ast.BadExpr{From: p.missing(), To: p.missing()}
	}
	p.next()
	return &ast.BadExpr{From: from, To: p.prevEnd}
}

func (p *parser) parseNewID() ast.Expr {
	x := &ast.NewIDExpr{New: p.tok.Pos}
	p.next()
	if p.layout && (p.at(token.COMMA) || p.at(token.RPAREN)) {
		return x // orderId: new — the type comes from the parameter
	}
	if !p.at(token.IDENT) {
		p.errorExpected(wantIDType)
		return &ast.BadExpr{From: x.New, To: p.prevEnd}
	}
	t := &ast.NamedType{Path: []*ast.Ident{p.parseIdent()}}
	for p.at(token.DOT) && p.peek(1).Kind == token.IDENT {
		p.next()
		t.Path = append(t.Path, p.parseIdent())
	}
	x.Type = t
	if len(t.Path) < 2 || t.Path[len(t.Path)-1].Name != "Id" {
		p.error(source.Range{Start: t.Pos(), End: t.End()}, diag.E0012, wantIDType, "'"+t.String()+"'")
	}
	return x
}

// parseString parses a string; a string with interpolation becomes an InterpString.
func (p *parser) parseString() ast.Expr {
	tok := p.tok
	p.next()
	interp := false
	for _, part := range tok.Parts {
		interp = interp || part.IsExpr
	}
	if !interp {
		var b strings.Builder
		for _, part := range tok.Parts {
			b.WriteString(part.Text)
		}
		return &ast.BasicLit{ValuePos: tok.Pos, Kind: token.STRING, Raw: tok.Text, Value: b.String()}
	}
	s := &ast.InterpString{Quote: tok.Pos, EndPos: tok.End}
	for _, part := range tok.Parts {
		if !part.IsExpr {
			s.Parts = append(s.Parts, ast.InterpPart{Text: part.Text})
			continue
		}
		sub := &parser{file: p.file, toks: part.Tokens, errs: p.errs, tok: part.Tokens[0], prevEnd: part.Pos + 1}
		x := sub.parseExpr()
		if !sub.at(token.EOF) {
			sub.errorExpected(quote("}"))
		}
		s.Parts = append(s.Parts, ast.InterpPart{X: x})
	}
	return s
}

// clauseOrder lists search sections and their order; page and limit are mutually exclusive.
var clauseOrder = map[string]int{"where": 1, "sort": 2, "page": 3, "limit": 3, "select": 4}

// isSearchStart: [Type] followed by search sections on the next lines.
func (p *parser) isSearchStart() bool {
	return p.at(token.IDENT) && isUpper(p.tok.Text) &&
		p.peek(1).Kind == token.RBRACK &&
		p.peek(2).Kind == token.NEWLINE &&
		p.peek(3).Kind == token.INDENT &&
		p.peek(4).Kind == token.IDENT && clauseOrder[p.peek(4).Text] > 0
}

func (p *parser) parseBracket() ast.Expr {
	lb := p.tok.Pos
	p.next()
	switch {
	case p.at(token.COLON) && p.peek(1).Kind == token.RBRACK:
		p.next()
		m := &ast.MapLit{Lbrack: lb, Rbrack: p.tok.Pos}
		p.next()
		return m
	case p.at(token.RBRACK):
		b := &ast.BracketExpr{Lbrack: lb, Rbrack: p.tok.Pos}
		p.next()
		return b
	case p.isSearchStart():
		return p.parseSearch(lb)
	}
	first := p.parseExpr()
	if p.at(token.COLON) {
		return p.parseMap(lb, first)
	}
	b := &ast.BracketExpr{Lbrack: lb, Elems: []ast.Expr{first}}
	for p.at(token.COMMA) {
		p.next()
		if p.at(token.RBRACK) {
			break // a comma after the last element
		}
		b.Elems = append(b.Elems, p.parseExpr())
	}
	b.Rbrack = p.expectClose(token.RBRACK)
	return b
}

func (p *parser) parseMap(lb source.Pos, firstKey ast.Expr) *ast.MapLit {
	m := &ast.MapLit{Lbrack: lb}
	key := firstKey
	for {
		e := &ast.MapEntry{Key: key, Colon: p.expect(token.COLON)}
		e.Value = p.parseExpr()
		m.Entries = append(m.Entries, e)
		if !p.at(token.COMMA) {
			break
		}
		p.next()
		if p.at(token.RBRACK) {
			break
		}
		key = p.parseExpr()
	}
	m.Rbrack = p.expectClose(token.RBRACK)
	return m
}

// parseSearch parses [Type] with sections on the next lines:
//
//	[Order]
//	  where customer == me
//	  sort placedAt desc
//	  page pageReq
//	  select OrderRow(...)
//
// If sections repeat or come out of order, the search becomes a BadExpr:
// some sections were dropped, and the search checks (indexes, page/limit)
// would report false errors.
func (p *parser) parseSearch(lb source.Pos) ast.Expr {
	s := &ast.SearchExpr{Lbrack: lb, Type: p.parseIdent()}
	bad := false
	s.Rbrack = p.expectClose(token.RBRACK)
	p.next() // NEWLINE
	p.next() // INDENT
	stage := 0
	p.blockLoop(func() {
		order, ok := clauseOrder[p.tok.Text]
		if !p.at(token.IDENT) || !ok {
			p.errorExpected(wantClause)
			p.syncLine()
			return
		}
		switch {
		case order == stage:
			p.error(p.tokRange(), diag.E0018, p.tok.Text)
			p.syncLine()
			bad = true
			return
		case order < stage:
			p.error(p.tokRange(), diag.E0021, p.tok.Text)
			p.syncLine()
			bad = true
			return
		}
		stage = order
		pos, word := p.tok.Pos, p.tok.Text
		p.next()
		switch word {
		case "where":
			s.WherePos, s.Where = pos, p.parseExpr()
		case "sort":
			s.SortPos, s.Sort = pos, p.parseSortKeys()
		case "page":
			s.PagePos, s.Page = pos, p.parseExpr()
		case "limit":
			s.LimitPos, s.Limit = pos, p.parseExpr()
		case "select":
			s.SelectPos, s.Select = pos, p.parseExpr()
		}
		p.expectNewline()
	})
	s.EndPos = p.prevEnd
	p.expectDedent()
	p.lineDone = true
	if bad {
		return &ast.BadExpr{From: s.Pos(), To: s.End()}
	}
	return s
}
