package lsp

import (
	"encoding/json"
	"path/filepath"
	"sort"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

func (s *server) definition(params json.RawMessage) (any, *rpcError) {
	var p textDocumentPositionParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	snap, ref := s.file(p.TextDocument.URI)
	if ref == nil {
		return nil, nil
	}
	_, t := newResolver(ref.model).at(ref.file.AST, filePos(ref.file.Src, p.Position))
	if t == nil || t.def == nil {
		return nil, nil
	}
	if loc, ok := location(snap.mod.FileSet, t.def); ok {
		return []Location{loc}, nil
	}
	return nil, nil
}

func (s *server) references(params json.RawMessage) (any, *rpcError) {
	var p referenceParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	snap, ref := s.file(p.TextDocument.URI)
	if ref == nil {
		return []Location{}, nil
	}
	_, t := newResolver(ref.model).at(ref.file.AST, filePos(ref.file.Src, p.Position))
	if t == nil || t.key() == nil {
		return []Location{}, nil
	}
	key := t.key()
	locs := []Location{}
	snap.eachFile(func(f *module.File, r *resolver) {
		walkIdents(f.AST, func(path []ast.Node) {
			id := last(path).(*ast.Ident)
			if id == key && !p.Context.IncludeDeclaration {
				return
			}
			if t := r.resolve(path); t != nil && t.key() == key {
				if loc, ok := location(snap.mod.FileSet, id); ok {
					locs = append(locs, loc)
				}
			}
		})
	})
	return locs, nil
}

// eachFile calls fn for every module file with name resolution in its
// model: base files in the base model, extension files in the model with
// the extension. Base trees are shared by all models, so references from
// extensions to base declarations match by pointer.
func (s *snapshot) eachFile(fn func(f *module.File, r *resolver)) {
	r := newResolver(s.base)
	for _, f := range s.mod.Base {
		fn(f, r)
	}
	names := make([]string, 0, len(s.extensions))
	for name := range s.extensions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if t := s.mod.Extension(name); t != nil {
			r := newResolver(s.extensions[name])
			for _, f := range t.Files {
				fn(f, r)
			}
		}
	}
}

// walkIdents calls fn for every name of a file with the node path to it.
func walkIdents(f *ast.File, fn func(path []ast.Node)) {
	var path []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			path = path[:len(path)-1]
			return false
		}
		path = append(path, n)
		if _, ok := n.(*ast.Ident); ok {
			fn(path)
		}
		return true
	})
}

// location returns the place of a name in a document.
func location(fset *source.FileSet, id *ast.Ident) (Location, bool) {
	f := fset.File(id.Pos())
	if f == nil {
		return Location{}, false
	}
	return Location{
		URI:   uriFromPath(filepath.FromSlash(f.Name())),
		Range: lspRange(f, source.Range{Start: id.Pos(), End: id.End()}),
	}, true
}
