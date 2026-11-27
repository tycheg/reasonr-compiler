// Package token defines ReasonR tokens and keywords.
package token

import (
	"strconv"

	"github.com/tycheg/reasonr-compiler/internal/source"
)

// Kind is a token kind.
type Kind uint8

const (
	ILLEGAL Kind = iota
	EOF

	// Block structure.
	NEWLINE
	INDENT
	DEDENT

	// Names and literals.
	IDENT
	INT       // 42, 1_000_000
	DECIMAL   // 10.50
	DURATION  // 500ms, 30s, 5m, 2h, 1d
	STRING    // "text {expression}"
	DATE      // date"2026-10-02"
	TIMESTAMP // timestamp"2026-10-02T12:00:00Z"

	// Brackets and separators.
	LPAREN // (
	RPAREN // )
	LBRACK // [
	RBRACK // ]
	COMMA  // ,
	DOT    // .
	COLON  // :

	// Operators.
	ASSIGN   // =
	EQ       // ==
	NE       // !=
	LT       // <
	LE       // <=
	GT       // >
	GE       // >=
	ADD      // +
	SUB      // -
	MUL      // *
	QUO      // /
	REM      // %
	QUESTION // ?
	SAFEDOT  // ?.
	COALESCE // ??
	PIPE     // | — type variants in primitive declarations
	HASH     // # before a number in a text key: AdminOrder:Order.Cancel#2

	keywordBeg
	// Reserved words (RFC §6.4).
	AGGREGATE
	ENTITY
	ACTOR
	VALUE
	ENUM
	PROJECTION
	EXTERNAL
	ROLE
	APP
	SCREEN
	PRIMITIVE
	TEXTS
	MIGRATION
	TEST
	EXTEND
	COMMAND
	EVENT
	NOTIFICATION
	ERROR
	QUERY
	RULE
	REQUEST
	INDEX
	LET
	IF
	ELSE
	FOR
	IN
	WHILE
	BREAK
	CONTINUE
	RETURN
	DELETE
	FORGET
	AND
	OR
	NOT
	IS
	NONE
	TRUE
	FALSE
	NEW
	keywordEnd
)

var names = [...]string{
	ILLEGAL:   "ILLEGAL",
	EOF:       "EOF",
	NEWLINE:   "NEWLINE",
	INDENT:    "INDENT",
	DEDENT:    "DEDENT",
	IDENT:     "IDENT",
	INT:       "INT",
	DECIMAL:   "DECIMAL",
	DURATION:  "DURATION",
	STRING:    "STRING",
	DATE:      "DATE",
	TIMESTAMP: "TIMESTAMP",

	LPAREN: "(",
	RPAREN: ")",
	LBRACK: "[",
	RBRACK: "]",
	COMMA:  ",",
	DOT:    ".",
	COLON:  ":",

	ASSIGN:   "=",
	EQ:       "==",
	NE:       "!=",
	LT:       "<",
	LE:       "<=",
	GT:       ">",
	GE:       ">=",
	ADD:      "+",
	SUB:      "-",
	MUL:      "*",
	QUO:      "/",
	REM:      "%",
	QUESTION: "?",
	SAFEDOT:  "?.",
	COALESCE: "??",
	PIPE:     "|",
	HASH:     "#",

	AGGREGATE:    "aggregate",
	ENTITY:       "entity",
	ACTOR:        "actor",
	VALUE:        "value",
	ENUM:         "enum",
	PROJECTION:   "projection",
	EXTERNAL:     "external",
	ROLE:         "role",
	APP:          "app",
	SCREEN:       "screen",
	PRIMITIVE:    "primitive",
	TEXTS:        "texts",
	MIGRATION:    "migration",
	TEST:         "test",
	EXTEND:       "extend",
	COMMAND:      "command",
	EVENT:        "event",
	NOTIFICATION: "notification",
	ERROR:        "error",
	QUERY:        "query",
	RULE:         "rule",
	REQUEST:      "request",
	INDEX:        "index",
	LET:          "let",
	IF:           "if",
	ELSE:         "else",
	FOR:          "for",
	IN:           "in",
	WHILE:        "while",
	BREAK:        "break",
	CONTINUE:     "continue",
	RETURN:       "return",
	DELETE:       "delete",
	FORGET:       "forget",
	AND:          "and",
	OR:           "or",
	NOT:          "not",
	IS:           "is",
	NONE:         "none",
	TRUE:         "true",
	FALSE:        "false",
	NEW:          "new",
}

func (k Kind) String() string {
	if int(k) < len(names) && names[k] != "" {
		return names[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// IsKeyword reports whether the kind is a reserved word.
func (k Kind) IsKeyword() bool { return keywordBeg < k && k < keywordEnd }

// IsLiteral reports whether the kind is a literal.
func (k Kind) IsLiteral() bool { return INT <= k && k <= TIMESTAMP }

// IsOperator reports whether the kind is an operator or a separator.
func (k Kind) IsOperator() bool { return LPAREN <= k && k <= HASH }

var keywords = func() map[string]Kind {
	m := make(map[string]Kind, keywordEnd-keywordBeg)
	for k := keywordBeg + 1; k < keywordEnd; k++ {
		m[names[k]] = k
	}
	return m
}()

// Lookup returns the kind of a reserved word, or IDENT.
func Lookup(name string) Kind {
	if k, ok := keywords[name]; ok {
		return k
	}
	return IDENT
}

// Contextual lists contextual keywords (RFC §6.4). The lexer emits them as
// IDENT; they become keywords only in their position.
var Contextual = []string{
	"id", "unique", "personal", "check", "timeout", "was", "on", "fake", "acl", "create",
	"where", "sort", "page", "limit", "select", "desc", "all",
	"after", "hasErrors", "now", "today", "me", "caller", "schedule", "self",
	"given", "when", "then", "as", "matches", "time", "passes", "exclude", "because", "to",
	"platforms", "language", "default", "auto", "back", "binds", "part", "children", "params", "inputs", "screens", "field", "input",
}

// Token is a token.
type Token struct {
	Kind Kind
	Pos  source.Pos // start
	End  source.Pos // end (exclusive); equals Pos for INDENT and DEDENT
	Text string     // source text of the token
	// Parts are the parts of a STRING: text and interpolated expressions.
	Parts []StringPart
}

// StringPart is a part of a string literal.
type StringPart struct {
	Pos, End source.Pos
	// Text is the decoded text of the part (if IsExpr == false).
	Text string
	// IsExpr: the part is an interpolation {expression}.
	IsExpr bool
	// Tokens are the expression's tokens; the last is EOF in place of "}".
	Tokens []Token
}

// Comment is a comment. Doc is a documentation comment "##".
type Comment struct {
	Pos, End source.Pos
	Text     string // text including "#" or "##"
	Doc      bool
}
