// Package lexer splits source text into tokens.
//
// Blocks are set by indentation: the lexer turns it into INDENT and DEDENT
// tokens and line ends into NEWLINE. Line breaks inside parentheses and
// square brackets are ignored so long calls can be wrapped. Comments do not
// go into the token stream; they are returned separately.
package lexer

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

// IndentStep is the indentation step of a new block.
const IndentStep = 2

// Scan splits a file into tokens. The token stream always ends with EOF,
// before which all blocks are closed (DEDENT).
func Scan(f *source.File) (toks []token.Token, comments []token.Comment, diags []*diag.Diagnostic) {
	s := &scanner{file: f, src: f.Source(), indents: []int{0}}
	// A token takes about 4 bytes of source on average: no reallocation
	// as it grows (docs/plans/0015).
	s.toks = make([]token.Token, 0, len(s.src)/4+16)
	s.run()
	return s.toks, s.comments, s.diags.List()
}

type bracket struct {
	kind   token.Kind // LPAREN or LBRACK
	indent int        // indentation of the line where the bracket opened
}

type scanner struct {
	file *source.File
	src  []byte
	off  int

	toks     []token.Token
	comments []token.Comment
	diags    diag.Bag

	indents    []int     // stack of indentation levels
	brackets   []bracket // open brackets
	lineIndent int       // indentation of the current logical line
	lineTokens bool      // the current logical line has tokens
}

func (s *scanner) pos(off int) source.Pos { return s.file.Pos(off) }

func (s *scanner) rng(start, end int) source.Range {
	return source.Range{Start: s.pos(start), End: s.pos(end)}
}

func (s *scanner) errorf(start, end int, spec *diag.Spec, args ...any) *diag.Diagnostic {
	return s.diags.Add(spec, s.rng(start, end), args...)
}

func (s *scanner) emit(k token.Kind, start, end int) {
	s.toks = append(s.toks, token.Token{
		Kind: k,
		Pos:  s.pos(start),
		End:  s.pos(end),
		Text: string(s.src[start:end]),
	})
}

func (s *scanner) run() {
	if len(s.src) >= 3 && s.src[0] == 0xEF && s.src[1] == 0xBB && s.src[2] == 0xBF { // BOM
		s.off = 3
	}
	atLineStart := true
	for {
		if atLineStart {
			s.lineStart()
			atLineStart = false
		}
		s.skipSpaces()
		if s.off >= len(s.src) {
			break
		}
		switch c := s.src[s.off]; c {
		case '\n':
			if len(s.brackets) == 0 && s.lineTokens {
				s.emit(token.NEWLINE, s.off, s.off+1)
				s.lineTokens = false
			}
			s.off++
			atLineStart = true
		case '#':
			if s.hashAfterName() {
				s.emit(token.HASH, s.off, s.off+1)
				s.off++
				continue
			}
			s.comment()
		default:
			tok, ok := s.scanToken(false)
			if !ok {
				continue
			}
			s.toks = append(s.toks, tok)
			s.lineTokens = true
			s.trackBrackets(tok.Kind)
		}
	}

	end := len(s.src)
	if s.lineTokens {
		s.emit(token.NEWLINE, end, end)
	}
	for len(s.indents) > 1 {
		s.indents = s.indents[:len(s.indents)-1]
		s.emit(token.DEDENT, end, end)
	}
	s.emit(token.EOF, end, end)
}

// hashAfterName reports that "#" is a node number in a text key
// (AdminOrder:Order.Cancel#2): it follows a name immediately and precedes a
// digit. A comment is separated from code by a space.
func (s *scanner) hashAfterName() bool {
	n := len(s.toks)
	if n == 0 || s.toks[n-1].Kind != token.IDENT || s.file.Offset(s.toks[n-1].End) != s.off {
		return false
	}
	return s.off+1 < len(s.src) && isDigit(s.src[s.off+1])
}

func (s *scanner) trackBrackets(k token.Kind) {
	switch k {
	case token.LPAREN, token.LBRACK:
		s.brackets = append(s.brackets, bracket{kind: k, indent: s.lineIndent})
	case token.RPAREN, token.RBRACK:
		// The parser handles mismatched brackets; only the depth matters here.
		if n := len(s.brackets); n > 0 {
			s.brackets = s.brackets[:n-1]
		}
	}
}

func (s *scanner) skipSpaces() {
	for s.off < len(s.src) {
		switch s.src[s.off] {
		case ' ', '\t', '\r':
			s.off++
		default:
			return
		}
	}
}

// lineStart handles the start of a physical line: it skips blank lines and
// comment-only lines, measures the indentation and emits INDENT/DEDENT.
func (s *scanner) lineStart() {
	for {
		lineStart := s.off
		col, tabAt := 0, -1
	measure:
		for s.off < len(s.src) {
			switch s.src[s.off] {
			case ' ':
				col++
			case '\t':
				col += IndentStep // to keep parsing, count a tab as one indentation step
				if tabAt < 0 {
					tabAt = s.off
				}
			case '\r':
			default:
				break measure
			}
			s.off++
		}
		if s.off >= len(s.src) {
			return
		}
		switch s.src[s.off] {
		case '\n': // a blank line
			s.off++
			continue
		case '#': // a comment-only line does not affect indentation
			s.comment()
			if s.off < len(s.src) {
				s.off++
			}
			continue
		}
		if tabAt >= 0 {
			s.errorf(tabAt, tabAt+1, diag.E0002)
		}
		if len(s.brackets) > 0 {
			if col > s.brackets[0].indent || !s.startsBlockKeyword() {
				return // an expression continues inside brackets
			}
			// The bracket is probably not closed: a new statement or
			// declaration starts no further right than the line with the
			// bracket. Close the brackets so as not to swallow the rest of the
			// file; the parser reports the error.
			s.brackets = s.brackets[:0]
			if s.lineTokens {
				// NEWLINE goes right after the last token: that is where the bracket is missing.
				end := s.file.Offset(s.toks[len(s.toks)-1].End)
				s.emit(token.NEWLINE, end, end)
				s.lineTokens = false
			}
		}
		s.indent(col, lineStart)
		return
	}
}

// indent compares the line's indentation with the stack of levels.
func (s *scanner) indent(col, lineStart int) {
	s.lineIndent = col
	top := s.indents[len(s.indents)-1]
	switch {
	case col > top:
		if col-top != IndentStep {
			s.errorf(lineStart, s.off, diag.E0003, col-top)
		}
		s.indents = append(s.indents, col)
		s.emit(token.INDENT, s.off, s.off)
	case col < top:
		for len(s.indents) > 1 && col < s.indents[len(s.indents)-1] {
			s.indents = s.indents[:len(s.indents)-1]
			s.emit(token.DEDENT, s.off, s.off)
		}
		if col != s.indents[len(s.indents)-1] {
			// A line between levels counts as a line of the nearest outer block.
			s.errorf(lineStart, s.off, diag.E0004)
			s.lineIndent = s.indents[len(s.indents)-1]
		}
	}
}

// blockKeywords are the words that start declarations and statements.
var blockKeywords = map[token.Kind]bool{
	token.AGGREGATE: true, token.ENTITY: true, token.ACTOR: true, token.VALUE: true, token.ENUM: true,
	token.PROJECTION: true, token.EXTERNAL: true, token.ROLE: true, token.APP: true,
	token.SCREEN: true, token.PRIMITIVE: true, token.TEXTS: true, token.MIGRATION: true, token.TEST: true,
	token.EXTEND: true, token.COMMAND: true, token.EVENT: true, token.NOTIFICATION: true,
	token.ERROR: true, token.QUERY: true, token.RULE: true, token.REQUEST: true,
	token.INDEX: true, token.LET: true, token.IF: true, token.ELSE: true, token.FOR: true,
	token.WHILE: true, token.BREAK: true, token.CONTINUE: true, token.RETURN: true,
	token.DELETE: true, token.FORGET: true,
}

func (s *scanner) startsBlockKeyword() bool {
	end := s.off
	for end < len(s.src) {
		r, n := utf8.DecodeRune(s.src[end:])
		if !isIdentRune(r) {
			break
		}
		end += n
	}
	return end > s.off && blockKeywords[token.Lookup(string(s.src[s.off:end]))]
}

// comment reads a comment to the end of the line (without the line break).
func (s *scanner) comment() {
	start := s.off
	for s.off < len(s.src) && s.src[s.off] != '\n' {
		s.off++
	}
	end := s.off
	if end > start && s.src[end-1] == '\r' {
		end--
	}
	text := string(s.src[start:end])
	s.comments = append(s.comments, token.Comment{
		Pos:  s.pos(start),
		End:  s.pos(end),
		Text: text,
		Doc:  strings.HasPrefix(text, "##"),
	})
}

func isLetter(r rune) bool {
	return r == '_' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || r >= utf8.RuneSelf && unicode.IsLetter(r)
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func isIdentRune(r rune) bool {
	return isLetter(r) || '0' <= r && r <= '9' || r >= utf8.RuneSelf && unicode.IsDigit(r)
}

func (s *scanner) peek(n int) byte {
	if s.off+n < len(s.src) {
		return s.src[s.off+n]
	}
	return 0
}

// scanToken reads one token starting at a non-space character.
// ok == false means the character was skipped with an error.
func (s *scanner) scanToken(inInterp bool) (tok token.Token, ok bool) {
	start := s.off
	c := s.src[s.off]
	r, size := rune(c), 1
	if c >= utf8.RuneSelf {
		r, size = utf8.DecodeRune(s.src[s.off:])
	}
	switch {
	case isLetter(r):
		return s.scanIdent(), true
	case isDigit(c):
		return s.scanNumber(), true
	case c == '"':
		return s.scanString(start, inInterp, token.STRING), true
	}

	kind := token.ILLEGAL
	s.off += size
	two := func(next byte, k2, k1 token.Kind) token.Kind {
		if s.off < len(s.src) && s.src[s.off] == next {
			s.off++
			return k2
		}
		return k1
	}
	switch c {
	case '(':
		kind = token.LPAREN
	case ')':
		kind = token.RPAREN
	case '[':
		kind = token.LBRACK
	case ']':
		kind = token.RBRACK
	case ',':
		kind = token.COMMA
	case '.':
		kind = token.DOT
	case ':':
		kind = token.COLON
	case '=':
		kind = two('=', token.EQ, token.ASSIGN)
	case '<':
		kind = two('=', token.LE, token.LT)
	case '>':
		kind = two('=', token.GE, token.GT)
	case '+':
		kind = token.ADD
	case '-':
		kind = token.SUB
	case '*':
		kind = token.MUL
	case '/':
		kind = token.QUO
	case '%':
		kind = token.REM
	case '?':
		switch s.peek(0) {
		case '.':
			s.off++
			kind = token.SAFEDOT
		case '?':
			s.off++
			kind = token.COALESCE
		default:
			kind = token.QUESTION
		}
	case '!':
		if s.peek(0) == '=' {
			s.off++
			kind = token.NE
			break
		}
		s.errorf(start, s.off, diag.E0001, "!").Hint(hintNot)
		return tok, false
	case '|':
		if s.peek(0) != '|' {
			kind = token.PIPE
			break
		}
		s.off++
		s.errorf(start, s.off, diag.E0001, "||").Hint(hintOr)
		return tok, false
	case '&':
		if s.peek(0) == c {
			s.off++
		}
		s.errorf(start, s.off, diag.E0001, string(s.src[start:s.off])).Hint(hintAnd)
		return tok, false
	default:
		s.errorf(start, s.off, diag.E0001, string(r))
		return tok, false
	}
	return token.Token{Kind: kind, Pos: s.pos(start), End: s.pos(s.off), Text: string(s.src[start:s.off])}, true
}

var (
	hintNot = `use "not" for negation and "!=" for inequality`
	hintAnd = `use "and"`
	hintOr  = `use "or"`
)

func (s *scanner) scanIdent() token.Token {
	start := s.off
	for s.off < len(s.src) {
		r, n := utf8.DecodeRune(s.src[s.off:])
		if !isIdentRune(r) {
			break
		}
		s.off += n
	}
	name := string(s.src[start:s.off])
	if s.peek(0) == '"' {
		switch name {
		case "date":
			return s.scanTimeLiteral(start, token.DATE, name, "2006-01-02", "YYYY-MM-DD")
		case "timestamp":
			return s.scanTimeLiteral(start, token.TIMESTAMP, name, time.RFC3339Nano, "YYYY-MM-DDThh:mm:ssZ")
		}
	}
	return token.Token{Kind: token.Lookup(name), Pos: s.pos(start), End: s.pos(s.off), Text: name}
}

// scanTimeLiteral reads date"…" or timestamp"…". The prefix starts at
// start; the current position is at the quote.
func (s *scanner) scanTimeLiteral(start int, kind token.Kind, name, layout, human string) token.Token {
	s.off++ // "
	valStart := s.off
	for s.off < len(s.src) && s.src[s.off] != '"' && s.src[s.off] != '\n' {
		s.off++
	}
	val := string(s.src[valStart:s.off])
	if s.off >= len(s.src) || s.src[s.off] != '"' {
		s.errorf(start, s.off, diag.E0005)
	} else {
		s.off++
		if _, err := time.Parse(layout, val); err != nil {
			s.errorf(start, s.off, diag.E0006, name, human)
		}
	}
	return token.Token{Kind: kind, Pos: s.pos(start), End: s.pos(s.off), Text: string(s.src[start:s.off])}
}

var (
	reasonLeadingZero  = "leading zero"
	reasonUnderscore   = `"_" is allowed only between digits`
	reasonNoFraction   = "missing digits after the decimal point"
	reasonSuffix       = "unknown suffix; durations are 500ms, 30s, 5m, 2h, 1d"
	reasonFracDuration = "a duration must be an integer: e.g. 90m instead of 1.5h"
)

var durationUnits = map[string]bool{"ms": true, "s": true, "m": true, "h": true, "d": true}

func (s *scanner) scanNumber() token.Token {
	start := s.off
	kind := token.INT
	var reason *string

	s.scanDigits(&reason)
	if s.src[start] == '0' && s.off-start > 1 && reason == nil {
		reason = &reasonLeadingZero
	}
	if s.peek(0) == '.' {
		switch next := s.peek(1); {
		case isDigit(next):
			s.off++
			kind = token.DECIMAL
			s.scanDigits(&reason)
		case next == '_' || next >= utf8.RuneSelf || 'a' <= next && next <= 'z' || 'A' <= next && next <= 'Z':
			// 1.foo: the dot is member access; the parser handles it.
		default:
			s.off++
			if reason == nil {
				reason = &reasonNoFraction
			}
		}
	}
	if r, _ := utf8.DecodeRune(s.src[s.off:]); s.off < len(s.src) && isLetter(r) {
		sufStart := s.off
		for s.off < len(s.src) {
			r, n := utf8.DecodeRune(s.src[s.off:])
			if !isIdentRune(r) {
				break
			}
			s.off += n
		}
		switch {
		case !durationUnits[string(s.src[sufStart:s.off])]:
			if reason == nil {
				reason = &reasonSuffix
			}
		case kind == token.DECIMAL:
			kind = token.DURATION
			if reason == nil {
				reason = &reasonFracDuration
			}
		default:
			kind = token.DURATION
		}
	}
	text := string(s.src[start:s.off])
	if reason != nil {
		s.errorf(start, s.off, diag.E0007, text, *reason)
	}
	return token.Token{Kind: kind, Pos: s.pos(start), End: s.pos(s.off), Text: text}
}

// scanDigits reads digits with "_" separators.
func (s *scanner) scanDigits(reason **string) {
	prevDigit := false
	for s.off < len(s.src) {
		c := s.src[s.off]
		switch {
		case isDigit(c):
			prevDigit = true
		case c == '_':
			if (!prevDigit || !isDigit(s.peek(1))) && *reason == nil {
				*reason = &reasonUnderscore
			}
			prevDigit = false
		default:
			return
		}
		s.off++
	}
}

var (
	interpNested = `a string inside "{ }" cannot be interpolated`
	interpEmpty  = `empty "{}"; literal braces are written as "{{" and "}}"`
	interpBrace  = `single "}"; a literal brace is written as "}}"`
)

// scanString reads a string literal with interpolation. inInterp: the
// string is inside "{ }" of another string, and interpolation is not allowed in it.
func (s *scanner) scanString(start int, inInterp bool, kind token.Kind) token.Token {
	s.off++ // the opening quote
	var (
		parts     []token.StringPart
		text      strings.Builder
		textStart = s.off
		nested    bool // inside "{" of a nested string already reported
	)
	flush := func(end int) {
		if end > textStart {
			parts = append(parts, token.StringPart{Pos: s.pos(textStart), End: s.pos(end), Text: text.String()})
		}
		text.Reset()
	}
	for {
		if s.off >= len(s.src) || s.src[s.off] == '\n' {
			s.errorf(start, s.off, diag.E0005)
			flush(s.off)
			break
		}
		c := s.src[s.off]
		switch {
		case c == '"':
			flush(s.off)
			s.off++
			return token.Token{Kind: kind, Pos: s.pos(start), End: s.pos(s.off), Text: string(s.src[start:s.off]), Parts: parts}
		case c == '\\':
			s.scanEscape(&text)
		case c == '{' && s.peek(1) == '{':
			text.WriteByte('{')
			s.off += 2
		case c == '}' && s.peek(1) == '}':
			text.WriteByte('}')
			s.off += 2
		case c == '}':
			if nested {
				nested = false // the pair of a "{" already reported
			} else {
				s.errorf(s.off, s.off+1, diag.E0008, interpBrace)
			}
			text.WriteByte('}')
			s.off++
		case c == '{' && inInterp:
			s.errorf(s.off, s.off+1, diag.E0008, interpNested)
			nested = true
			text.WriteByte('{')
			s.off++
		case c == '{':
			flush(s.off)
			open := s.off
			s.off++
			// If "{" is not closed, neither is the string: the next iteration reports E0005.
			toks := s.scanInterp(open)
			parts = append(parts, token.StringPart{Pos: s.pos(open), End: s.pos(s.off), IsExpr: true, Tokens: toks})
			textStart = s.off
		default:
			_, n := utf8.DecodeRune(s.src[s.off:])
			text.Write(s.src[s.off : s.off+n])
			s.off += n
		}
	}
	return token.Token{Kind: kind, Pos: s.pos(start), End: s.pos(s.off), Text: string(s.src[start:s.off]), Parts: parts}
}

// scanInterp reads the tokens of an expression inside "{ }". It returns the
// tokens; the last one is EOF in place of "}".
func (s *scanner) scanInterp(open int) (toks []token.Token) {
	for {
		s.skipSpaces()
		if s.off >= len(s.src) || s.src[s.off] == '\n' {
			return append(toks, token.Token{Kind: token.EOF, Pos: s.pos(s.off), End: s.pos(s.off)})
		}
		if s.src[s.off] == '}' {
			if len(toks) == 0 {
				s.errorf(open, s.off+1, diag.E0008, interpEmpty)
			}
			toks = append(toks, token.Token{Kind: token.EOF, Pos: s.pos(s.off), End: s.pos(s.off)})
			s.off++
			return toks
		}
		if s.src[s.off] == '"' {
			toks = append(toks, s.scanString(s.off, true, token.STRING))
			continue
		}
		if s.src[s.off] == '#' {
			s.errorf(s.off, s.off+1, diag.E0001, "#")
			s.off++
			continue
		}
		if tok, ok := s.scanToken(true); ok {
			toks = append(toks, tok)
		}
	}
}

// scanEscape parses an escape sequence; the position is at "\".
func (s *scanner) scanEscape(text *strings.Builder) {
	start := s.off
	s.off++
	if s.off >= len(s.src) || s.src[s.off] == '\n' {
		s.errorf(start, s.off, diag.E0009, "\\")
		return
	}
	switch s.src[s.off] {
	case '"':
		text.WriteByte('"')
		s.off++
		return
	case '\\':
		text.WriteByte('\\')
		s.off++
		return
	case 'n':
		text.WriteByte('\n')
		s.off++
		return
	case 't':
		text.WriteByte('\t')
		s.off++
		return
	case 'u':
		if s.peek(1) != '{' {
			break
		}
		end := s.off + 2
		for end < len(s.src) && isHex(s.src[end]) {
			end++
		}
		closed := end < len(s.src) && s.src[end] == '}'
		if closed && end > s.off+2 && end-s.off-2 <= 6 {
			var v rune
			for _, h := range s.src[s.off+2 : end] {
				v = v<<4 | rune(hexVal(h))
			}
			if v <= unicode.MaxRune && !(0xD800 <= v && v <= 0xDFFF) {
				text.WriteRune(v)
				s.off = end + 1
				return
			}
		}
		if closed {
			end++
		}
		s.off = end
		s.errorf(start, s.off, diag.E0009, string(s.src[start:s.off]))
		return
	}
	_, n := utf8.DecodeRune(s.src[s.off:])
	s.off += n
	s.errorf(start, s.off, diag.E0009, string(s.src[start:s.off]))
}

func isHex(c byte) bool {
	return isDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func hexVal(c byte) int {
	switch {
	case isDigit(c):
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}
