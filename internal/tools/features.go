package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/sema"
)

// Source feature flags (RFC §23.4): reasonr feature list shows the
// model's flags with their date, age, description, place and the tests that
// turn them on. reasonrd feature list shows the rollout in production.

// FeatureRow is a source flag.
type FeatureRow struct {
	Name    string   `json:"name"` // the full name: Ordering.tips
	System  string   `json:"system,omitempty"`
	Date    string   `json:"date"`
	AgeDays int      `json:"ageDays"`
	Doc     string   `json:"doc,omitempty"`
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Reads   int      `json:"reads"`           // how many places read the flag: code and layout
	Tests   []string `json:"tests,omitempty"` // tests that turn the flag on
	Models  []string `json:"models"`          // base and the extensions that have the flag
}

// Features returns the flags of the models (base and extensions) by name;
// now is for the age.
func Features(models []*pipeline.Model, now time.Time) []FeatureRow {
	rows := map[string]*FeatureRow{}
	for _, m := range models {
		name := m.Program.Extension
		if name == "" {
			name = "base"
		}
		reads := map[string]map[*ast.Ident]bool{}
		for id, o := range m.Info.Uses {
			if o.Kind == sema.ObjFeature {
				if reads[o.Name] == nil {
					reads[o.Name] = map[*ast.Ident]bool{}
				}
				reads[o.Name][id] = true
			}
		}
		tests := map[string][]string{}
		for d, names := range m.Info.TestFeatures {
			for _, n := range names {
				tests[n] = append(tests[n], d.Name.Value)
				for _, id := range d.Features.Names {
					delete(reads[n], id) // a feature line is not a read
				}
			}
		}
		for _, full := range sema.FeatureList(m.Info) {
			f := m.Info.Features[full]
			row := rows[full]
			if row == nil {
				pos := m.Module.FileSet.Position(f.Name.Pos())
				row = &FeatureRow{Name: full, Date: f.Date.Value, Doc: f.Doc.Text(), File: pos.Filename, Line: pos.Line}
				if sys, _, ok := strings.Cut(full, "."); ok {
					row.System = sys
				}
				if d, err := time.Parse(time.DateOnly, f.Date.Value); err == nil {
					row.AgeDays = int(now.Sub(d).Hours() / 24)
				}
				rows[full] = row
			}
			row.Models = append(row.Models, name)
			row.Reads = max(row.Reads, len(reads[full]))
			for _, t := range tests[full] {
				if !contains(row.Tests, t) {
					row.Tests = append(row.Tests, t)
				}
			}
		}
	}
	out := make([]FeatureRow, 0, len(rows))
	for _, r := range rows {
		sort.Strings(r.Tests)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// WriteFeatures prints the flags as a table or as JSON (format json); paths
// are relative to dir.
func WriteFeatures(w io.Writer, rows []FeatureRow, format, dir string) error {
	for i := range rows {
		if rel, err := filepath.Rel(dir, rows[i].File); err == nil {
			rows[i].File = rel
		}
	}
	if format == "json" {
		b, err := json.MarshalIndent(struct {
			Version  int          `json:"version"`
			Features []FeatureRow `json:"features"`
		}{1, rows}, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(b))
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "FLAG\tDATE\tAGE\tREADS\tTESTS\tPLACE\tDESCRIPTION")
	for _, r := range rows {
		doc, _, _ := strings.Cut(r.Doc, "\n")
		fmt.Fprintf(tw, "%s\t%s\t%dd\t%d\t%d\t%s:%d\t%s\n", r.Name, r.Date, r.AgeDays, r.Reads, len(r.Tests), r.File, r.Line, doc)
	}
	return tw.Flush()
}
