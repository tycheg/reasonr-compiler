package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/apicontract"
	"github.com/tycheg/reasonr-compiler/internal/apidoc"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/schemagen"
)

// External API governance (RFC §31.3.1): the contract api/contract.json
// and the versions api/versions/<date>.json next to the model sources.

var apiDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// apiCmd — reasonr api publish|unpublish|release|deprecate|retire|openapi|list.
func apiCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "reasonr: api: name a command: publish, unpublish, release, deprecate, retire, openapi or list")
		return 2
	}
	fl := flag.NewFlagSet("api "+args[0], flag.ContinueOnError)
	fl.SetOutput(stderr)
	date := fl.String("date", "", "version date YYYY-MM-DD (release; today by default)")
	sunset := fl.String("sunset", "", "the date after which the method may be removed (deprecate)")
	version := fl.String("version", "", "API version (openapi)")
	if err := fl.Parse(args[1:]); err != nil {
		return 2
	}
	rest, dir := fl.Args(), defaultPath()
	// The last directory argument is the module.
	if n := len(rest); n > 0 {
		if st, err := os.Stat(rest[n-1]); err == nil && st.IsDir() {
			dir, rest = rest[n-1], rest[:n-1]
		}
	}
	dirs, err := module.Find(dir)
	if err != nil || len(dirs) != 1 {
		fmt.Fprintf(stderr, "reasonr: api: %s must hold one module\n", dir)
		return 2
	}
	dir = dirs[0]
	switch args[0] {
	case "publish", "unpublish":
		if len(rest) == 0 {
			fmt.Fprintf(stderr, "reasonr: api %s: name methods: Ordering.Order.Cancel or Ordering.Order.*\n", args[0])
			return 2
		}
		return editContract(dir, stdout, stderr, func(c *apicontract.Contract) {
			for _, p := range rest {
				if args[0] == "publish" && !slices.Contains(c.Publish, p) {
					c.Publish = append(c.Publish, p)
				}
				if args[0] == "unpublish" {
					c.Publish = slices.DeleteFunc(c.Publish, func(x string) bool { return x == p })
				}
			}
			sort.Strings(c.Publish)
		})
	case "deprecate":
		if len(rest) != 1 || !apiDate.MatchString(*sunset) {
			fmt.Fprintln(stderr, "reasonr: api deprecate: name a method and --sunset YYYY-MM-DD")
			return 2
		}
		return editContract(dir, stdout, stderr, func(c *apicontract.Contract) {
			c.Deprecate = slices.DeleteFunc(c.Deprecate, func(d model.APIDeprecation) bool { return d.Method == rest[0] })
			c.Deprecate = append(c.Deprecate, model.APIDeprecation{Method: rest[0], Sunset: *sunset})
			sort.Slice(c.Deprecate, func(i, j int) bool { return c.Deprecate[i].Method < c.Deprecate[j].Method })
		})
	case "retire":
		if len(rest) != 1 || !apiDate.MatchString(rest[0]) {
			fmt.Fprintln(stderr, "reasonr: api retire: name the version date YYYY-MM-DD")
			return 2
		}
		p := filepath.Join(dir, apicontract.Dir, "versions", rest[0]+".json")
		if err := os.Remove(p); err != nil {
			fmt.Fprintf(stderr, "reasonr: api retire: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "version %s retired: %s removed\n", rest[0], p)
		return 0
	}
	m, code := loadModel(dir, "", true, stderr)
	if m == nil {
		return code
	}
	if m.HasErrors() {
		diag.WriteText(stderr, m.Module.FileSet, m.Diagnostics)
		return 1
	}
	s := schemagen.Generate(m.Program, m.Info)
	if s.API == nil {
		fmt.Fprintf(stderr, "reasonr: %s has no contract %s/contract.json: publish methods (reasonr api publish)\n", dir, apicontract.Dir)
		return 1
	}
	switch args[0] {
	case "release":
		d := *date
		if d == "" {
			d = time.Now().Format(time.DateOnly)
		}
		if !apiDate.MatchString(d) {
			fmt.Fprintln(stderr, "reasonr: api release: --date YYYY-MM-DD")
			return 2
		}
		v := apicontract.Snapshot(s, s.API, d)
		p := filepath.Join(dir, apicontract.Dir, "versions", d+".json")
		if err := writeJSONFile(p, v); err != nil {
			fmt.Fprintf(stderr, "reasonr: api release: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "version %s: %d methods → %s\n", d, len(v.Methods), p)
		return 0
	case "openapi":
		sc := s
		if *version != "" {
			v := s.API.Version(*version)
			if v == nil {
				fmt.Fprintf(stderr, "reasonr: api openapi: no version %s\n", *version)
				return 2
			}
			sc = apicontract.View(s, v)
		}
		doc := apidoc.OpenAPI(sc, true)
		if *version != "" {
			doc["info"].(apidoc.Obj)["version"] = *version
		}
		out, _ := json.MarshalIndent(doc, "", "  ")
		fmt.Fprintln(stdout, string(out))
		return 0
	case "list":
		return apiList(stdout, m, s)
	}
	fmt.Fprintf(stderr, "reasonr: api: unknown command %q\n", args[0])
	return 2
}

// editContract changes api/contract.json of module dir (creates it if absent).
func editContract(dir string, stdout, stderr io.Writer, edit func(*apicontract.Contract)) int {
	p := filepath.Join(dir, apicontract.Dir, "contract.json")
	c := apicontract.Contract{Version: 1, Publish: []string{}}
	data, err := os.ReadFile(p)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &c); err != nil {
			fmt.Fprintf(stderr, "reasonr: %s: %v\n", p, err)
			return 1
		}
	case !errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 1
	}
	edit(&c)
	if err := writeJSONFile(p, c); err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s: %d published, %d deprecated\n", p, len(c.Publish), len(c.Deprecate))
	return 0
}

func writeJSONFile(p string, v any) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o644)
}

// apiList prints the contract: methods, versions, deprecations.
func apiList(w io.Writer, m *pipeline.Model, s *model.Schema) int {
	fmt.Fprintln(w, "Published:")
	for _, full := range s.API.Methods {
		line := "  " + full
		if d := s.API.Deprecation(full); d != nil {
			line += " (deprecated, remove after " + d.Sunset + ")"
		}
		fmt.Fprintln(w, line)
	}
	var versions []string
	for _, v := range s.API.Versions {
		versions = append(versions, v.Date)
	}
	if len(versions) == 0 {
		versions = []string{"none — reasonr api release"}
	}
	fmt.Fprintf(w, "Versions: %s\n", strings.Join(versions, ", "))
	_ = m
	return 0
}
