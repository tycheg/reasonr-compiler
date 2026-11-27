package diag

import (
	"embed"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Detailed explanations of codes for `reasonr diag explain` live in
// explain/*.md. A section looks like this:
//
//	## E0504
//
//	The explanation (a paragraph up to a blank line).
//
//	```rr
//	a model with the error
//	```
//
//	```rr
//	the fixed model
//	```
//
// An example is a module of one file m.rr; a line "# file: path" starts
// the next file of the module (extensions in extensions/<name>/, model
// versions old/ and new/ for compatibility codes). A "```rr skip" block
// is not checked by the test.

//go:embed explain/*.md
var explainFS embed.FS

// Explanation is a detailed explanation of a code with an example and a fix.
type Explanation struct {
	Code    Code
	Text    string
	Example []ExampleFile
	Fix     []ExampleFile
	Skip    bool // the examples are not checked automatically
}

// ExampleFile is a file of an example.
type ExampleFile struct {
	Path string
	Src  string
}

var explanations = func() map[Code]*Explanation {
	m, err := parseExplanations(explainFS)
	if err != nil {
		panic(err)
	}
	return m
}()

// Explain returns the explanation of a code or nil.
func Explain(c Code) *Explanation { return explanations[c] }

var (
	headingRe = regexp.MustCompile(`^## ([EW]\d{4})\s*$`)
	fileRe    = regexp.MustCompile(`^# file: (\S+)\s*$`)
)

func parseExplanations(fsys embed.FS) (map[Code]*Explanation, error) {
	out := map[Code]*Explanation{}
	files, err := fsys.ReadDir("explain")
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		data, err := fsys.ReadFile(path.Join("explain", f.Name()))
		if err != nil {
			return nil, err
		}
		if err := parseFile(f.Name(), string(data), out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func parseFile(name, text string, out map[Code]*Explanation) error {
	var cur *Explanation
	para := false
	var block []string
	inBlock, skip := false, false
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		where := fmt.Sprintf("%s:%d", name, i+1)
		switch {
		case inBlock && line == "```":
			inBlock = false
			files := splitFiles(block)
			switch {
			case cur.Example == nil:
				cur.Example = files
			case cur.Fix == nil:
				cur.Fix = files
			default:
				return fmt.Errorf("diag: %s: %s has more than two examples", where, cur.Code)
			}
			cur.Skip = cur.Skip || skip
		case inBlock:
			block = append(block, line)
		case headingRe.MatchString(line):
			code := Code(headingRe.FindStringSubmatch(line)[1])
			if out[code] != nil {
				return fmt.Errorf("diag: %s: %s is explained twice", where, code)
			}
			cur = &Explanation{Code: code}
			out[code] = cur
			para = false
		case cur == nil:
			// Text before the first section is the file heading.
		case strings.HasPrefix(line, "```rr"):
			inBlock, block = true, nil
			skip = strings.TrimSpace(strings.TrimPrefix(line, "```rr")) == "skip"
			para = false
		case strings.TrimSpace(line) == "":
			para = false
		case para:
			cur.Text += " " + strings.TrimSpace(line)
		case cur.Text == "":
			cur.Text, para = strings.TrimSpace(line), true
		default:
			return fmt.Errorf("diag: %s: unexpected line %q", where, line)
		}
	}
	if inBlock {
		return fmt.Errorf("diag: %s: a code block is not closed", name)
	}
	return nil
}

// splitFiles splits a block into files at "# file: path" lines.
func splitFiles(lines []string) []ExampleFile {
	var out []ExampleFile
	cur := ExampleFile{Path: "m.rr"}
	var body []string
	flush := func() {
		for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
			body = body[:len(body)-1]
		}
		if len(body) > 0 || cur.Path != "m.rr" {
			cur.Src = strings.Join(body, "\n") + "\n"
			out = append(out, cur)
		}
	}
	for _, l := range lines {
		if m := fileRe.FindStringSubmatch(l); m != nil {
			flush()
			cur, body = ExampleFile{Path: m[1]}, nil
			continue
		}
		body = append(body, l)
	}
	flush()
	return out
}

// Explained returns the codes that have an explanation, in ascending order.
func Explained() []Code {
	var out []Code
	for c := range explanations {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// WriteExplanation prints the explanation of a code for a person.
func WriteExplanation(w io.Writer, c Code) error {
	s := Lookup(c)
	if s == nil {
		return fmt.Errorf("unknown code %s", c)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s: %s\n", s.Code, s.Severity, s.Title)
	if e := Explain(c); e != nil {
		b.WriteString("\n" + wrap(e.Text, 80) + "\n")
		writeFiles(&b, "Example", e.Example)
		writeFiles(&b, "Fix", e.Fix)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeFiles(b *strings.Builder, title string, files []ExampleFile) {
	if len(files) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s:\n", title)
	for _, f := range files {
		if len(files) > 1 {
			fmt.Fprintf(b, "\n    # %s\n", f.Path)
		}
		for _, l := range strings.Split(strings.TrimRight(f.Src, "\n"), "\n") {
			if l == "" {
				b.WriteString("\n")
				continue
			}
			b.WriteString("    " + l + "\n")
		}
	}
}

// wrap wraps text at word boundaries.
func wrap(text string, width int) string {
	var b strings.Builder
	n := 0
	for _, w := range strings.Fields(text) {
		l := len([]rune(w))
		if n > 0 && n+1+l > width {
			b.WriteByte('\n')
			n = 0
		} else if n > 0 {
			b.WriteByte(' ')
			n++
		}
		b.WriteString(w)
		n += l
	}
	return b.String()
}
