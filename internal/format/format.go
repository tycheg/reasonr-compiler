// Package format brings ReasonR source text to canonical form (the reasonr
// fmt command).
//
// The formatter prints the syntax tree anew and places comments by their
// source positions: own-line comments between block elements, trailing
// ones at the end of their line. Blank lines between elements are kept
// (several in a row become one) and removed at the start of a block. The
// formatter does not change the program's meaning: the tree of the
// formatted text equals the source tree ignoring positions.
package format

import (
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/parser"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

// Source parses and formats a file. If it has syntax errors, it returns the
// source text unchanged and the diagnostics.
func Source(fset *source.FileSet, name string, src []byte) ([]byte, []*diag.Diagnostic) {
	f := fset.AddFile(name, src)
	file, diags := parser.Parse(f)
	if diag.HasErrors(diags) {
		return src, diags
	}
	return File(f, file), diags
}

// File prints the tree of file f, parsed without errors.
func File(f *source.File, file *ast.File) []byte {
	p := &printer{src: f, comments: file.Comments, used: make([]bool, len(file.Comments))}
	out := render(p.file(file))
	if out == "" {
		return nil
	}
	return []byte(out + "\n")
}

type printer struct {
	src      *source.File
	comments []token.Comment
	used     []bool
	start    int // all comments before start are already printed
	lastLine int // the last source line already in the output
}

func (p *printer) line(pos source.Pos) int { return p.src.Line(pos) }

// col returns the column of a position in runes, from 0.
func (p *printer) col(pos source.Pos) int { return p.src.Position(pos).Column - 1 }

func (p *printer) use(i int) {
	p.used[i] = true
	for p.start < len(p.used) && p.used[p.start] {
		p.start++
	}
}

func commentText(c token.Comment) string { return strings.TrimRight(c.Text, " \t\r") }

// firstFrom returns the index of the first comment that starts no earlier than pos.
func (p *printer) firstFrom(pos source.Pos) int {
	return sort.Search(len(p.comments), func(i int) bool { return p.comments[i].Pos >= pos })
}

// hasComments reports whether there are unprinted comments in [from, to).
func (p *printer) hasComments(from, to source.Pos) bool {
	for i := p.firstFrom(from); i < len(p.comments) && p.comments[i].Pos < to; i++ {
		if !p.used[i] {
			return true
		}
	}
	return false
}

// trailing returns the trailing comment of the line where after stands, if
// it exists and is not printed yet. If unprinted comments remain before it
// (inside an expression rather than between elements), it does not become
// trailing: they are all printed later as own lines in order.
func (p *printer) trailing(after source.Pos) doc {
	i := p.firstFrom(after)
	if i == len(p.comments) || p.used[i] || p.line(p.comments[i].Pos) != p.line(after) || p.start < i {
		return nil
	}
	p.use(i)
	c := p.comments[i]
	return suffix{text: commentText(c), col: p.col(c.Pos)}
}

// blankBefore reports whether the source had a blank line between the last
// printed line and pos.
func (p *printer) blankBefore(pos source.Pos) bool {
	for l := p.lastLine + 1; l < p.line(pos); l++ {
		if strings.TrimSpace(p.src.LineText(l)) == "" {
			return true
		}
	}
	return false
}

// nextCode returns the start of the first line after end that has code (not
// blank and not only a comment), or the end of the file.
func (p *printer) nextCode(end source.Pos) source.Pos {
	for l := p.line(end) + 1; l <= p.src.LineCount(); l++ {
		t := strings.TrimSpace(p.src.LineText(l))
		if t != "" && t[0] != '#' {
			return p.src.LineStart(l)
		}
	}
	return p.src.Pos(p.src.Size())
}

// lines collects the lines of a block: elements and the comments between them.
type lines struct {
	p   *printer
	out []blockLine
	// sep: separate elements with a body (declarations and members) by a blank line.
	sep      bool
	items    int
	prevBody bool
}

type blockLine struct {
	blank bool // a blank line before the line
	d     doc
}

func (l *lines) comment(i int) {
	p := l.p
	c := p.comments[i]
	p.use(i)
	l.out = append(l.out, blockLine{blank: p.blankBefore(c.Pos), d: text(commentText(c))})
	p.lastLine = max(p.lastLine, p.line(c.Pos))
}

// comments adds all unprinted comments up to position to.
func (l *lines) comments(to source.Pos) {
	p := l.p
	for i := p.start; i < len(p.comments) && p.comments[i].Pos < to; i++ {
		if !p.used[i] {
			l.comment(i)
		}
	}
}

// add adds an element with positions [pos, end): first the comments before
// it, then the element itself and its trailing comment.
func (l *lines) add(pos, end source.Pos, build func() doc) {
	l.addItem(pos, end, false, build)
}

// addItem is like add; hasBody reports that the element has a body. In
// blocks with sep such an element is separated from its neighbors by a
// blank line, placed before the element's comments so the documentation
// stays with it.
func (l *lines) addItem(pos, end source.Pos, hasBody bool, build func() doc) {
	p := l.p
	first := len(l.out)
	l.comments(pos)
	blank := p.blankBefore(pos)
	d := build()
	d = cat(d, p.trailing(end))
	p.lastLine = max(p.lastLine, p.line(end))
	l.out = append(l.out, blockLine{blank: blank, d: d})
	if l.sep && l.items > 0 && (hasBody || l.prevBody) {
		l.out[first].blank = true
	}
	l.items++
	l.prevBody = hasBody
}

// tail adds the comments after the last element of a block that come
// before the next code line and belong to the block by indentation.
// Documentation right above the next code line stays with the next element.
func (l *lines) tail(minCol int, end source.Pos) {
	p := l.p
	boundary := p.nextCode(end)
	for i := p.firstFrom(end); i < len(p.comments) && p.comments[i].Pos < boundary; i++ {
		if p.used[i] {
			continue
		}
		if p.col(p.comments[i].Pos) < minCol || p.docOf(i, boundary) {
			break
		}
		l.comment(i)
	}
}

// docOf reports whether comment i starts a "##" group right above code line
// next, and so documents it.
func (p *printer) docOf(i int, next source.Pos) bool {
	if !p.comments[i].Doc || next == p.src.Pos(p.src.Size()) {
		return false
	}
	last := p.line(p.comments[i].Pos)
	for j := i + 1; j < len(p.comments) && p.comments[j].Pos < next; j++ {
		if !p.comments[j].Doc || p.line(p.comments[j].Pos) != last+1 {
			break
		}
		last++
	}
	return p.line(next) == last+1
}

func (l *lines) doc() doc {
	var parts concat
	for i, ln := range l.out {
		if i > 0 {
			parts = append(parts, hardline)
			if ln.blank {
				parts = append(parts, hardline)
			}
		}
		parts = append(parts, ln.d)
	}
	return parts
}

// block prints the elements of a block, each on a new line.
func block[T ast.Node](p *printer, items []T, f func(T) doc) doc {
	l := &lines{p: p}
	for _, it := range items {
		l.add(it.Pos(), it.End(), func() doc { return f(it) })
	}
	if len(items) > 0 {
		l.tail(p.col(items[0].Pos()), items[len(items)-1].End())
	}
	return l.doc()
}

// body is an indented block under a heading.
func body[T ast.Node](p *printer, items []T, f func(T) doc) doc {
	return indent(hardline, block(p, items, f))
}

// members is a block of members: members with a body are separated by blank lines.
func members[T ast.Node](p *printer, items []T, f func(T) doc) doc {
	l := &lines{p: p, sep: true}
	for _, it := range items {
		l.addItem(it.Pos(), it.End(), hasBody(it), func() doc { return f(it) })
	}
	if len(items) > 0 {
		l.tail(p.col(items[0].Pos()), items[len(items)-1].End())
	}
	return indent(hardline, l.doc())
}

// hasBody reports whether a declaration or a member has an indented body.
func hasBody(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.EnumDecl, *ast.ValueDecl, *ast.EntityDecl, *ast.ProjectionDecl, *ast.ExternalDecl, *ast.FakeDecl, *ast.ACLDecl,
		*ast.AppDecl, *ast.ScreenDecl, *ast.PrimitiveDecl, *ast.TextsDecl, *ast.MigrationDecl, *ast.TestDecl, *ast.ExtendDecl,
		*ast.CommandDecl, *ast.QueryDecl, *ast.RuleDecl, *ast.OnDecl, *ast.MethodDecl:
		return true
	case *ast.EventDecl:
		return n.Body != nil
	case *ast.RequestDecl:
		return n.Body != nil
	}
	return false
}

// ownLines prints unprinted comments up to position to as own lines; each
// ends with a line break.
func (p *printer) ownLines(to source.Pos) doc {
	l := &lines{p: p}
	l.comments(to)
	var parts concat
	for _, ln := range l.out {
		parts = append(parts, ln.d, hardline)
	}
	return parts
}

func (p *printer) file(f *ast.File) doc {
	l := &lines{p: p, sep: true}
	for _, d := range f.Decls {
		l.addItem(d.Pos(), d.End(), hasBody(d), func() doc { return p.decl(d) })
	}
	l.comments(p.src.Pos(p.src.Size()))
	return l.doc()
}
