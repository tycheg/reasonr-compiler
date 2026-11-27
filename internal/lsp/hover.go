package lsp

import (
	"encoding/json"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/check"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

var (
	lblParamOf     = "parameter of %s"
	lblFieldOf     = "field of %s"
	lblLocal       = "variable"
	lblBuiltin     = "built-in"
	lblPersonal    = "Personal data of %s"
	lblPersonalAny = "Personal data of any subject"
	lblFeature     = "feature flag, created %s: rollout — reasonrd feature list"
)

func (s *server) hover(params json.RawMessage) (any, *rpcError) {
	var p textDocumentPositionParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	_, ref := s.file(p.TextDocument.URI)
	if ref == nil {
		return nil, nil
	}
	src := ref.file.Src
	r := newResolver(ref.model)
	id, t := r.at(ref.file.AST, filePos(src, p.Position))
	if t == nil {
		return nil, nil
	}
	text := s.describe(r, t)
	if text == "" {
		return nil, nil
	}
	rng := lspRange(src, source.Range{Start: id.Pos(), End: id.End()})
	return hover{Contents: markupContent{Kind: "markdown", Value: text}, Range: &rng}, nil
}

// describe returns a description of the target in Markdown: the
// declaration in a code block, whose member it is, the documentation and
// the personal data subject.
func (s *server) describe(r *resolver, t *target) string {
	tr := func(x string, args ...any) string { return diag.Message{Format: x, Args: args}.String() }
	var code, note string
	var doc *ast.CommentGroup
	switch t.kind {
	case tLocal:
		code, note = "let "+t.name+" "+typeString(t.typ), tr(lblLocal)
	case tBuiltin:
		code, note = t.name, tr(lblBuiltin)
		if t.typ != nil && !isVoid(t.typ) && !types.IsInvalid(t.typ) {
			code += " " + t.typ.String()
		}
	case tParam:
		code = t.name + " " + typeString(t.typ)
		if t.v != nil {
			if p, ok := t.v.Node.(*ast.Param); ok {
				code = r.m.Source(rangeOf(p))
			}
		}
		if t.method != nil {
			note = tr(lblParamOf, "`"+t.decl.Name+"."+t.method.Name+"`")
		} else if t.decl != nil {
			note = tr(lblParamOf, "`"+t.decl.Name+"`")
		}
	case tField:
		code = t.name + " " + typeString(t.typ)
		if t.v != nil {
			if f, ok := t.v.Node.(*ast.FieldDecl); ok {
				code, doc = r.m.Source(rangeOf(f)), f.Doc
			}
		}
		if t.decl != nil {
			note = tr(lblFieldOf, "`"+t.decl.Name+"`")
		}
	case tEnumValue:
		code, doc = t.decl.Name+"."+t.name, t.value.Node.Doc
	case tDecl:
		code, doc = declHeader(r, t.decl), docOf(t.decl.Node)
	case tMethod:
		code, doc = r.m.Source(memberHeader(t.method.Node)), docOf(t.method.Node)
		if t.decl != nil {
			note = "`" + t.decl.Name + "`"
		}
	case tIndex:
		code, doc = r.m.Source(rangeOf(t.index.Node)), t.index.Node.Doc
		note = "`" + t.decl.Name + "`"
	case tRole:
		code, doc = "role "+t.name, t.role.Doc
	case tFeature:
		code, doc = "feature "+t.name+" "+t.flag.Date.Raw, t.flag.Doc
		note = tr(lblFeature, t.flag.Date.Value)
	case tNamespace:
		code = t.name // a system or a bounded context
	}
	var b strings.Builder
	b.WriteString("```reasonr\n" + code + "\n```")
	if note != "" {
		b.WriteString("\n\n" + note)
	}
	if text := doc.Text(); text != "" {
		b.WriteString("\n\n---\n\n" + text)
	}
	if t.v != nil && t.v.Personal {
		subject, anySubject := personalSubject(r, t.v)
		b.WriteString("\n\n")
		if anySubject {
			b.WriteString(tr(lblPersonalAny))
		} else if subject != nil {
			b.WriteString(tr(lblPersonal, "`"+subject.Name+"`"))
		}
	}
	return b.String()
}

// personalSubject returns the subject of a personal field or parameter; any
// means an external request parameter (any subject).
func personalSubject(r *resolver, v *types.Var) (subject *types.Decl, anySubject bool) {
	pm, _ := r.m.Checks.Of(check.PII).(*check.PIIMap)
	if pm == nil {
		return nil, false
	}
	for _, list := range [][]check.PersonalVar{pm.Fields, pm.Params} {
		for _, p := range list {
			if p.Var == v {
				return p.Subject, p.Subject == nil
			}
		}
	}
	return nil, false
}

func typeString(t types.Type) string {
	if t == nil {
		return ""
	}
	return t.String()
}

func isVoid(t types.Type) bool {
	_, ok := t.(*types.Void)
	return ok
}

func rangeOf(n ast.Node) source.Range { return source.Range{Start: n.Pos(), End: n.End()} }

// declHeader returns the heading of a declaration: "aggregate Order", `screen AdminOrder(orderId Order.Id) "Order"`.
func declHeader(r *resolver, d *types.Decl) string {
	switch n := d.Node.(type) {
	case *ast.EntityDecl:
		switch {
		case n.IsActor:
			return "actor " + d.Name
		case n.IsAggregate:
			return "aggregate " + d.Name
		}
		return "entity " + d.Name
	case *ast.ScreenDecl:
		end := n.Name.End()
		if n.Params != nil {
			end = n.Params.End()
		}
		if n.Title != nil {
			end = n.Title.End()
		}
		return r.m.Source(source.Range{Start: n.Screen, End: end})
	case *ast.AppDecl:
		end := n.Name.End()
		if n.Title != nil {
			end = n.Title.End()
		}
		return r.m.Source(source.Range{Start: n.App, End: end})
	case *ast.PrimitiveDecl:
		end := n.Name.End()
		if n.Params != nil {
			end = n.Params.End()
		} else if n.TypeParams != nil {
			end = n.TypeParams.End()
		}
		return r.uiSource(source.Range{Start: n.Primitive, End: end})
	}
	return d.Kind.String() + " " + d.Name
}

// memberHeader returns the range of a member heading: without the body.
func memberHeader(m ast.Member) source.Range {
	switch n := m.(type) {
	case *ast.CommandDecl:
		return source.Range{Start: n.Command, End: n.Params.End()}
	case *ast.EventDecl:
		return source.Range{Start: n.Event, End: n.Params.End()}
	case *ast.QueryDecl:
		return source.Range{Start: n.Query, End: n.Result.End()}
	case *ast.RuleDecl:
		return source.Range{Start: n.Rule, End: n.Result.End()}
	case *ast.MethodDecl:
		return source.Range{Start: n.Name.Pos(), End: n.Result.End()}
	}
	return rangeOf(m)
}

// docOf returns the documentation (##) of a declaration or member.
func docOf(n ast.Node) *ast.CommentGroup {
	switch n := n.(type) {
	case *ast.EntityDecl:
		return n.Doc
	case *ast.ProjectionDecl:
		return n.Doc
	case *ast.ValueDecl:
		return n.Doc
	case *ast.EnumDecl:
		return n.Doc
	case *ast.ExternalDecl:
		return n.Doc
	case *ast.AppDecl:
		return n.Doc
	case *ast.ScreenDecl:
		return n.Doc
	case *ast.PrimitiveDecl:
		return n.Doc
	case *ast.FieldDecl:
		return n.Doc
	case *ast.IndexDecl:
		return n.Doc
	case *ast.CommandDecl:
		return n.Doc
	case *ast.EventDecl:
		return n.Doc
	case *ast.NotificationDecl:
		return n.Doc
	case *ast.ErrorDecl:
		return n.Doc
	case *ast.QueryDecl:
		return n.Doc
	case *ast.RuleDecl:
		return n.Doc
	case *ast.MethodDecl:
		return n.Doc
	case *ast.RequestDecl:
		return n.Doc
	}
	return nil
}
