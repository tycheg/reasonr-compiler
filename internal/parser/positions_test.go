package parser

import (
	"fmt"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// checkPositions checks that every node has Pos <= End, children lie inside
// the parent, and siblings do not overlap and come in order.
func checkPositions(t testing.TB, fset *source.FileSet, file *ast.File) {
	t.Helper()
	if err := positionsError(file); err != nil {
		t.Fatalf("%s: %v", file.Name, err)
	}
}

func positionsError(file *ast.File) (err error) {
	type frame struct {
		node    ast.Node
		lastEnd source.Pos
	}
	var stack []*frame
	ast.Inspect(file, func(n ast.Node) bool {
		if err != nil {
			return false
		}
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if _, ok := n.(*ast.CommentGroup); ok {
			return false // documentation lies outside the node
		}
		pos, end := n.Pos(), n.End()
		if !pos.IsValid() || end < pos {
			err = fmt.Errorf("%T: invalid range [%d, %d)", n, pos, end)
			return false
		}
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			if pos < parent.node.Pos() || end > parent.node.End() {
				err = fmt.Errorf("%T [%d, %d) outside parent %T [%d, %d)", n, pos, end, parent.node, parent.node.Pos(), parent.node.End())
				return false
			}
			if pos < parent.lastEnd {
				err = fmt.Errorf("%T [%d, %d) overlaps the previous sibling (end %d) in %T", n, pos, end, parent.lastEnd, parent.node)
				return false
			}
			parent.lastEnd = end
		}
		stack = append(stack, &frame{node: n})
		return true
	})
	return err
}
