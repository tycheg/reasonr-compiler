// Package rfcblocks extracts ```rr code blocks from the RFC.
package rfcblocks

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
