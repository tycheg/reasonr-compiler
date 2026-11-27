// Package rfcblocks extracts ```rr code blocks from the RFC.
package rfcblocks

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Block is an ```rr block of a markdown document.
type Block struct {
	Path string // the document the block is in
	Line int    // the document line where the code starts (after ```rr)
	Text string // the block content with a trailing line break
	File string // the path from a "# taxi/order.rr" heading, or "" without one
}

// Pos returns the block position as "path:line".
func (b Block) Pos() string { return b.Path + ":" + strconv.Itoa(b.Line) }

var headerRe = regexp.MustCompile(`^# (\S+\.rr)$`)

// Parse extracts ```rr blocks from markdown.
func Parse(md []byte) []Block {
	var (
		out    []Block
		cur    *strings.Builder
		start  int
		lineNo int
	)
	sc := bufio.NewScanner(bytes.NewReader(md))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		switch {
		case cur == nil && line == "```rr":
			cur = &strings.Builder{}
			start = lineNo + 1
		case cur != nil && line == "```":
			b := Block{Line: start, Text: cur.String()}
			first, _, _ := strings.Cut(b.Text, "\n")
			if m := headerRe.FindStringSubmatch(first); m != nil {
				b.File = m[1]
			}
			out = append(out, b)
			cur = nil
		case cur != nil:
			cur.WriteString(line)
			cur.WriteByte('\n')
		}
	}
	return out
}

// Load extracts the ```rr blocks of a markdown file, or of every *.md file
// of a directory in name order (the RFC chapters).
func Load(path string) ([]Block, error) {
	files, err := Files(path)
	if err != nil {
		return nil, err
	}
	var out []Block
	for _, f := range files {
		md, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, b := range Parse(md) {
			b.Path = f
			out = append(out, b)
		}
	}
	return out, nil
}

// Files returns path itself if it is a file, or the *.md files of the
// directory path in name order.
func Files(path string) ([]string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return []string{path}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, filepath.Join(path, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// Illustrative lists RFC blocks that are deliberately not code: a syntax
// template, signatures without bodies, a mix of declarations, members and
// statements. The key is the first line of the block.
var Illustrative = map[string]bool{
	"aggregate <Name>": true,
	// The inner entity in §10.7 is abridged; in full in §25.4.
	"# taxi/contexts/Dispatch/trip.offer.rr — in full in §25.4":                     true,
	"command(admin) Register(driverName string, driverCar Car)        # role admin": true,
	"notification orderPlaced(orderId Order.Id, price Money)":                       true,
	"migration fillOrderMart": true, // a migration and a command "# in Order" in one block
	// Taxi additions for the admin app (§22.3): commands with new access,
	// without bodies; in full in the §25 files.
	"# Taxi model additions for the admin app (in full in §25.1–25.3)": true,
}

// IsIllustrative reports whether a block is an illustration rather than code.
func (b Block) IsIllustrative() bool {
	first, _, _ := strings.Cut(b.Text, "\n")
	return Illustrative[first]
}

// Variants returns the variants of a block to parse: as a file, as entity
// members and as a command body. "..." lines are omitted.
func (b Block) Variants() []string {
	var lines []string
	for _, l := range strings.Split(b.Text, "\n") {
		if strings.TrimSpace(l) != "..." {
			lines = append(lines, l)
		}
	}
	text := strings.Join(lines, "\n")
	indent := func(s string, n int) string {
		pad := strings.Repeat(" ", n)
		out := strings.Split(s, "\n")
		for i, l := range out {
			if l != "" {
				out[i] = pad + l
			}
		}
		return strings.Join(out, "\n")
	}
	return []string{
		text,
		"aggregate X\n" + indent(text, 2),
		"aggregate X\n  command x()\n" + indent(text, 4),
	}
}

// RFC is the path to the RFC-0001 chapters: the link compiler/testdata/rfc
// to docs/rfc/0001-reasonr-language. The tests read the RFC through a path
// inside the module, otherwise go test would not notice an RFC edit and
// would return a cached result.
func RFC() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "rfc")
}

// layout is the version of the example directory layout, part of its key.
const layout = "4"

var examples = sync.OnceValue(func() string {
	dir, err := writeExamples()
	if err != nil {
		panic("rfcblocks: RFC examples: " + err.Error())
	}
	return dir
})

// Examples returns the directory with the modules of RFC examples: a block
// with the heading "# taxi/order.rr" is the file taxi/order.rr. The
// directory is shared by processes with the same RFC text; tests only read it.
func Examples() string { return examples() }

func writeExamples() (string, error) {
	files, err := Files(RFC())
	if err != nil {
		return "", err
	}
	var md []byte
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		md = append(md, data...)
	}
	// The key includes the repository path (reasonr.yaml of the examples
	// holds absolute replace paths, so a moved checkout needs a new
	// directory) and the layout version: bump it when writeExamples changes.
	sum := sha256.Sum256(append(append(md, RFC()...), layout...))
	dir := filepath.Join(os.TempDir(), "reasonr-rfc-"+hex.EncodeToString(sum[:8]))
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	tmp, err := os.MkdirTemp(os.TempDir(), "reasonr-rfc-")
	if err != nil {
		return "", err
	}
	for _, b := range Parse(md) {
		if b.File == "" {
			continue
		}
		p := filepath.Join(tmp, filepath.FromSlash(b.File))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, []byte(b.Text), 0o644); err != nil {
			return "", err
		}
	}
	if err := projectFiles(tmp); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		// Another process already wrote the directory.
		os.RemoveAll(tmp)
		if _, serr := os.Stat(dir); serr != nil {
			return "", err
		}
	}
	return dir, nil
}

// projectFiles copies reasonr.yaml and reasonr.lock of compiler/examples
// into the examples (project modules), with the sources at the root and
// absolute replace paths.
func projectFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		src := filepath.Join(filepath.Dir(RFC()), "..", "examples", e.Name())
		data, err := os.ReadFile(filepath.Join(src, "reasonr.yaml"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		replace := false
		for i, l := range lines {
			switch {
			case strings.HasPrefix(l, "src:"):
				// The RFC blocks are written at the root of the example.
				lines[i] = "src: ."
			case strings.HasPrefix(l, "replace:"):
				replace = true
			case replace && strings.HasPrefix(l, "  "):
				if k, v, ok := strings.Cut(l, ": "); ok && !filepath.IsAbs(strings.TrimSpace(v)) {
					abs, err := filepath.Abs(filepath.Join(src, strings.TrimSpace(v)))
					if err != nil {
						return err
					}
					lines[i] = k + ": " + filepath.ToSlash(abs)
				}
			case !strings.HasPrefix(l, " ") && l != "":
				replace = false
			}
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name(), "reasonr.yaml"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			return err
		}
		lock, err := os.ReadFile(filepath.Join(src, "reasonr.lock"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name(), "reasonr.lock"), lock, 0o644); err != nil {
			return err
		}
	}
	return nil
}
