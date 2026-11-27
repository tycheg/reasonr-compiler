package parser

import (
	"github.com/tycheg/reasonr-compiler/internal/token"
)

// What was expected, for E0012 messages.
var (
	wantName        = "a name"
	wantType        = "a type"
	wantExpr        = "an expression"
	wantNewline     = "end of line"
	wantDecl        = "a declaration: aggregate, actor, entity, value, enum, projection, external, fake, acl, role, feature, app, screen, primitive, default, texts, migration, test, extend or let"
	wantMember      = "a member: a field, index, command, event, notification, error, query or rule"
	wantValueMember = "a field, check or a method"
	wantRequest     = "'request'"
	wantFakeMember  = "a fake member: a field, command, event or request"
	wantACLMember   = "an ACL member: on Entity.event or actor Actor as Actor"
	wantNode        = "a layout line: a primitive, a field or on"
	wantPrimItem    = "binds, children, part or event"
	wantBindKind    = "what the primitive binds: a query result type, command, screen, field type or input type"
	wantPartKind    = "what names in the part bind: a type, params, inputs or screens"
	wantMethod      = "a method: Type.Method"
	wantTextKey     = "a text key: Type.member or Screen:binding"
	wantScreen      = "a screen or back"
	wantClause      = "a search clause: where, sort, page, limit or select"
	wantTestItem    = "feature, let, given, when or then"
	wantMatches     = "'matches'"
	wantString      = "a string"
	wantIDType      = "an id type: new Order.Id"
	wantStmt        = "a statement"
)

// Headings that need a body, for E0016 messages.
var (
	whatEnum       = "an enum header"
	whatValue      = "a value header"
	whatEntity     = "an aggregate or entity header"
	whatProjection = "a projection header"
	whatExternal   = "an external system header"
	whatFake       = "a fake header"
	whatACL        = "an acl header"
	whatRequest    = "a request implementation header"
	whatApp        = "an app header"
	whatScreen     = "a screen header"
	whatTexts      = "a texts header"
	whatIn         = "an in header"
	whatMigration  = "a migration header"
	whatTest       = "a test header"
	whatExtend     = "an extend header"
	whatCommand    = "a command header"
	whatEvent      = "an event header"
	whatQuery      = "a query header"
	whatRule       = "a rule header"
	whatMethod     = "a method header"
	whatOn         = "an on header"
	whatIf         = "if"
	whatElse       = "else"
	whatFor        = "for"
	whatWhile      = "while"
	whatSection    = "a section header"
)

var (
	noteReserved = "%q is a reserved word and cannot be used as a name"
	hintAssign   = `use "=" to assign; a comparison needs an if`
)

func quote(s string) string {
	return "'" + s + "'"
}

// describe describes the found token for an error message.
func describe(t token.Token) string {
	switch t.Kind {
	case token.NEWLINE:
		return "end of line"
	case token.INDENT:
		return "indentation"
	case token.DEDENT:
		return "end of block"
	case token.EOF:
		return "end of file"
	case token.IDENT:
		return "name '" + t.Text + "'"
	case token.STRING:
		return "a string"
	case token.INT, token.DECIMAL:
		return "number '" + t.Text + "'"
	case token.DURATION:
		return "duration '" + t.Text + "'"
	case token.DATE, token.TIMESTAMP:
		return "literal '" + t.Text + "'"
	}
	return quote(t.Kind.String())
}
