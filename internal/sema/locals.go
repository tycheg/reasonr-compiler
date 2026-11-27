package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// scope holds the names of the parameters and variables of a method: a chain of blocks.
type scope struct {
	c      *checker
	fields *types.Decl // fields the names must not match; nil when there are no fields
	blocks []map[string]*ast.Ident
}

func (c *checker) newScope(fields *types.Decl) *scope {
	return &scope{c: c, fields: fields, blocks: []map[string]*ast.Ident{{}}}
}

func (s *scope) push() { s.blocks = append(s.blocks, map[string]*ast.Ident{}) }
func (s *scope) pop()  { s.blocks = s.blocks[:len(s.blocks)-1] }

// declare declares a parameter or a variable in the current block.
func (s *scope) declare(id *ast.Ident) {
	c := s.c
	if id.Name == "" || c.builtinName(id) {
		return
	}
	if s.fields != nil {
		if f := s.fields.Field(id.Name); f != nil {
			c.bag.Add(diag.E0103, rangeOf(id), id.Name).Note(rangeOf(f.Node.(*ast.FieldDecl).Name), noteDeclared)
			return
		}
	}
	for _, b := range s.blocks {
		if prev := b[id.Name]; prev != nil {
			c.bag.Add(diag.E0104, rangeOf(id), id.Name).Note(rangeOf(prev), noteDeclared)
			return
		}
	}
	s.blocks[len(s.blocks)-1][id.Name] = id
}

func (s *scope) params(l *ast.ParamList) {
	if l == nil {
		return
	}
	for _, p := range l.List {
		s.declare(p.Name)
	}
}

// block checks the variables in a body; the body is a new block.
func (s *scope) block(b *ast.Block) {
	if b == nil {
		return
	}
	s.push()
	s.stmts(b.List)
	s.pop()
}

func (s *scope) stmts(list []ast.Stmt) {
	for _, st := range list {
		switch st := st.(type) {
		case *ast.LetStmt:
			s.c.lowerName(st.Name, kindVar)
			s.declare(st.Name)
		case *ast.IfStmt:
			for st != nil {
				s.block(st.Body)
				switch e := st.Else.(type) {
				case *ast.IfStmt:
					st = e
					continue
				case *ast.Block:
					s.block(e)
				}
				st = nil
			}
		case *ast.ForStmt:
			s.push()
			s.c.lowerName(st.Var, kindVar)
			s.declare(st.Var)
			s.block(st.Body)
			s.pop()
		case *ast.WhileStmt:
			s.block(st.Body)
		}
	}
}

// checkLocals checks the names of parameters and variables in all methods,
// migrations and tests.
func (c *checker) checkLocals(decls []ast.Decl) {
	fixtures := c.newScope(nil) // shared test lets
	for _, d := range decls {
		if l, ok := d.(*ast.LetDecl); ok {
			c.lowerName(l.Let.Name, kindVar)
			fixtures.declare(l.Let.Name)
		}
	}
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.EntityDecl:
			c.methodLocals(d.Members)
		case *ast.ProjectionDecl:
			c.methodLocals(d.Members)
		case *ast.ValueDecl:
			c.methodLocals(d.Members)
		case *ast.ExternalDecl:
			for _, m := range d.Members {
				if r, ok := m.(*ast.RequestDecl); ok && r.Body == nil {
					c.newScope(nil).params(r.Params)
				}
			}
			c.methodLocals(d.Members)
		case *ast.ScreenDecl:
			c.newScope(nil).params(d.Params)
		case *ast.MigrationDecl:
			c.newScope(nil).block(d.Body)
		case *ast.TestDecl:
			fixtures.push()
			fixtures.stmts(d.Setup)
			fixtures.pop()
		}
	}
}

// methodLocals checks the parameters and variables of the methods of a
// type. Parameters of errors and notifications may match fields: they have
// no body, and the parameters of a unique index error are named after the
// index fields.
func (c *checker) methodLocals(members []ast.Member) {
	for _, m := range members {
		owner := c.info.Owner[m]
		if owner == nil {
			continue
		}
		s := c.newScope(owner)
		switch m := m.(type) {
		case *ast.CommandDecl:
			s.params(m.Params)
			s.block(m.Body)
		case *ast.EventDecl:
			s.params(m.Params)
			s.block(m.Body)
		case *ast.QueryDecl:
			s.params(m.Params)
			s.block(m.Body)
		case *ast.RuleDecl:
			s.params(m.Params)
			s.block(m.Body)
		case *ast.MethodDecl:
			s.params(m.Params)
			s.block(m.Body)
		case *ast.RequestDecl:
			if m.Body != nil {
				s.params(m.Params)
				s.block(m.Body)
			}
		case *ast.OnDecl:
			// The event arguments are available in a reaction. They are
			// already checked in the event itself, so they only take the names.
			if ev := c.onEvent(owner, m); ev != nil {
				for _, p := range ev.Node.(*ast.EventDecl).Params.List {
					s.blocks[0][p.Name.Name] = p.Name
				}
			}
			s.block(m.Body)
		case *ast.NotificationDecl:
			c.newScope(nil).params(m.Params)
		case *ast.ErrorDecl:
			if m.Params != nil {
				c.newScope(nil).params(m.Params)
			}
		}
	}
}
