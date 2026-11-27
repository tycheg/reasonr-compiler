package format

import (
	"strings"
	"unicode/utf8"
)

// Wadler documents (as in Prettier): the printer builds a document tree and
// the renderer decides which groups fit on a line and which must break. A
// group prints on one line if it and the rest of the line up to the
// nearest possible break fit in maxWidth.

const (
	maxWidth   = 120
	indentStep = 2
)

type doc any

// text is text without line breaks.
type text string

// concat is a sequence of documents.
type concat []doc

// group is a group: on one line or with breaks at every line inside.
// brk: the group must break (it has a hard break inside).
type group struct {
	d   doc
	brk bool
}

// nest increases the indentation of the lines inside by indentStep.
type nest struct{ d doc }

type lineKind uint8

const (
	softLine  lineKind = iota // nothing on one line
	spaceLine                 // a space on one line
	hardLine                  // always a break
)

type line struct{ kind lineKind }

// ifBreak is different text in a broken group and in a one-line group.
type ifBreak struct{ brk, flat doc }

// suffix is a trailing comment: printed at the end of the current line.
// col is the comment's source column (in runes, from 0).
type suffix struct {
	text string
	col  int
}

var (
	softline  = line{softLine}
	spaceline = line{spaceLine}
	hardline  = line{hardLine}
)

func cat(ds ...doc) doc { return concat(ds) }

func grp(ds ...doc) doc {
	d := concat(ds)
	return &group{d: d, brk: hasHard(d)}
}

func indent(ds ...doc) doc { return &nest{d: concat(ds)} }

// hasHard reports whether the document has a hard break. It does not
// descend into nested groups: theirs is already computed.
func hasHard(d doc) bool {
	switch x := d.(type) {
	case concat:
		for _, e := range x {
			if hasHard(e) {
				return true
			}
		}
	case *group:
		return x.brk
	case *nest:
		return hasHard(x.d)
	case line:
		return x.kind == hardLine
	case *ifBreak:
		return hasHard(x.brk)
	}
	return false
}

func width(s string) int { return utf8.RuneCountInString(s) }

type cmd struct {
	ind  int
	flat bool
	d    doc
}

func render(root doc) string {
	var (
		out      strings.Builder
		col      int
		pending  = -1 // indentation to print before the text
		lineNo   int
		suffixes []suffix
		// Trailing comments by output line number: align gives them a column
		// once all lines are known.
		tails = map[int]tail{}
		cmds  = []cmd{{d: root}}
	)
	write := func(s string) {
		if pending >= 0 {
			out.WriteString(strings.Repeat(" ", pending))
			col = pending
			pending = -1
		}
		out.WriteString(s)
		col += width(s)
	}
	flushSuffixes := func() {
		if len(suffixes) == 0 {
			return
		}
		texts := make([]string, len(suffixes))
		for i, s := range suffixes {
			texts[i] = s.text
		}
		tails[lineNo] = tail{col: suffixes[0].col, comment: strings.Join(texts, " ")}
		suffixes = suffixes[:0]
	}
	for len(cmds) > 0 {
		c := cmds[len(cmds)-1]
		cmds = cmds[:len(cmds)-1]
		switch x := c.d.(type) {
		case nil:
		case text:
			if x != "" {
				write(string(x))
			}
		case concat:
			for i := len(x) - 1; i >= 0; i-- {
				cmds = append(cmds, cmd{c.ind, c.flat, x[i]})
			}
		case *nest:
			cmds = append(cmds, cmd{c.ind + indentStep, c.flat, x.d})
		case *group:
			flat := c.flat || !x.brk && fits(cmd{c.ind, true, x.d}, cmds, maxWidth-col)
			cmds = append(cmds, cmd{c.ind, flat, x.d})
		case line:
			if c.flat {
				if x.kind == spaceLine {
					write(" ")
				}
				continue
			}
			flushSuffixes()
			out.WriteByte('\n')
			lineNo++
			pending = c.ind
			col = c.ind
		case *ifBreak:
			if c.flat {
				cmds = append(cmds, cmd{c.ind, c.flat, x.flat})
			} else {
				cmds = append(cmds, cmd{c.ind, c.flat, x.brk})
			}
		case suffix:
			suffixes = append(suffixes, x)
		default:
			panic("format: unknown document")
		}
	}
	flushSuffixes()
	return align(out.String(), tails)
}

// fits reports whether next and the rest of the line from rest fit in w columns.
func fits(next cmd, rest []cmd, w int) bool {
	stack := []cmd{next}
	ri := len(rest)
	for w >= 0 {
		if len(stack) == 0 {
			if ri == 0 {
				return true
			}
			ri--
			stack = append(stack, rest[ri])
			continue
		}
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch x := c.d.(type) {
		case text:
			w -= width(string(x))
		case concat:
			for i := len(x) - 1; i >= 0; i-- {
				stack = append(stack, cmd{c.ind, c.flat, x[i]})
			}
		case *nest:
			stack = append(stack, cmd{c.ind, c.flat, x.d})
		case *group:
			stack = append(stack, cmd{c.ind, c.flat && !x.brk, x.d})
		case line:
			if !c.flat {
				return true
			}
			if x.kind == spaceLine {
				w--
			}
		case *ifBreak:
			if c.flat {
				stack = append(stack, cmd{c.ind, c.flat, x.flat})
			} else {
				stack = append(stack, cmd{c.ind, c.flat, x.brk})
			}
		}
	}
	return false
}

// tail is the trailing comment of an output line.
type tail struct {
	col     int // the comment's source column
	comment string
}

// align places trailing comments. Consecutive lines with trailing comments
// form a group aligned to one column: no further left than the comment's
// source column and no closer than two spaces to the code.
func align(s string, tails map[int]tail) string {
	if len(tails) == 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); {
		if _, ok := tails[i]; !ok {
			i++
			continue
		}
		j, col := i, 0
		for ; j < len(lines); j++ {
			t, ok := tails[j]
			if !ok {
				break
			}
			col = max(col, t.col, width(lines[j])+2)
		}
		for k := i; k < j; k++ {
			lines[k] += strings.Repeat(" ", col-width(lines[k])) + tails[k].comment
		}
		i = j
	}
	return strings.Join(lines, "\n")
}
