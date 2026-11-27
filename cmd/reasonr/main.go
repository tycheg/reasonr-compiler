// Command reasonr is the compiler and the tools of the ReasonR language.
//
// It checks modules (syntax, extensions, names, types, model rules,
// version compatibility), formats them, builds the IR, the schema, the
// bytecode and the UI program, offers model understanding tools and a
// language server (RFC §28). The commands that need the
// engine (tests, configuration checks, building a server) belong to reasonrd.
package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strings"
)

const usage = `Usage:
  reasonr check [--extension name] [--against schema.json] [--format text|json] [path ...]
  reasonr fmt [-w] [-l] [--check] [path ...]
  reasonr test [reasonrd test flags] [path[:line] ...]
  reasonr dev [reasonrd dev flags] [path]
  reasonr build [--extension name] [--dump-ir] [--dump-schema] [--dump-bytecode] [--dump-ui] [--dump-impact] [--ui-types [--platform web]] [-o file] [path]
  reasonr ui eject [--extension name] App [path]
  reasonr explain [--extension name] Type[.method] [path]
  reasonr graph [--extension name] [--entity Type | --app App | --contexts] [--format mermaid|dot] [path]
  reasonr pii [--extension name] [--format text|json] [path]
  reasonr extract Context [path]
  reasonr api publish|unpublish Method... [path]
  reasonr api release [--date YYYY-MM-DD] [path]
  reasonr api deprecate Method --sunset YYYY-MM-DD [path]
  reasonr api retire YYYY-MM-DD [path]
  reasonr api openapi [--version YYYY-MM-DD] [path]
  reasonr api list [path]
  reasonr impact [--base ref] [--format text|json] [--allow-test-changes test]... [path]
  reasonr feature list [--format text|json] [path]
  reasonr feature remove [--off] flag [path]
  reasonr diag explain CODE...
  reasonr diag list
  reasonr mod download | verify
  reasonr lsp

check checks modules. A module is a directory with *.rr files and
extensions in extensions/<name>/; in a directory without *.rr files modules
are looked up in nested directories. The base model and the models of all
extensions are checked; --extension name checks only the model with that extension, --extension base only the base one.
A separate *.rr file is checked for syntax only. --against compares
one model (the base one without --extension) with the schema of a previous version and reports
incompatible changes (E11xx, W11xx).

fmt prints the formatted text; -w writes it to the files, -l lists
the files that need formatting, --check does the same and
exits with code 1 if there are such files. Directories are walked recursively.

build checks a module and builds the IR and the bytecode: --dump-ir prints the IR, -o
writes it to a file in binary form, --dump-schema prints the model
schema as JSON, --dump-bytecode the disassembled bytecode; without flags
the IR hash is printed.

explain describes a type or a method: access, checks, actions in order, who
calls it. graph draws commands, events, calls, timers, external requests and
projections (Mermaid or Graphviz); --contexts draws the context map with ACLs.
api maintains the external API contract (the api/ directory next to the sources):
publish and unpublish change the contract's methods (Ordering.Order.Cancel or
Ordering.Order.*), release writes a version snapshot api/versions/<date>.json,
deprecate deprecates with a removal date, retire retires a version, openapi prints
OpenAPI 3.1 of the published API (--version as clients of a version see it), list
shows what is published. check compares the model with released versions (E1104–E1107).
impact lists the acceptance tests a change may affect
relative to a version in git (--base, origin/main by default): a test is affected
if the IR functions and types reachable from it include changed ones; it is an error
if the change introduces a feature flag and changes an old test.
extract analyzes extracting a bounded context into a separate process: ACL reactions across
its border become separate units of work, and errors of their commands no longer
cancel the source command. pii prints the personal data map:
fields and parameters by subject, where the values go, where the subject
is forgotten.

feature list lists the source feature flags: the date introduced, the age,
the description, the place, how many places read the flag and which tests turn it on.
reasonrd feature list shows the rollout in production. feature remove removes
a rolled-out flag from the code: conditions with it are replaced with the "on" branch (--off:
the "off" branch; the tests that turned the flag on are removed), and so are the declaration and the flag in
the feature lines of tests.

diag explain explains a diagnostic code with an example error and a fix,
diag list lists all codes.

lsp starts the Language Server Protocol server for editors (stdin and
stdout): diagnostics, formatting, hover, go to definition,
completion, quick fixes, semantic tokens.

test and dev start the project engine: acceptance tests (reasonrd test) and
local debugging (reasonrd dev). The project is reasonr.yaml in the model directory
or above: src holds the sources, platform.image is the platform image (docker;
dev brings up the platform's debug environment, databases and the engine, with docker
compose), platform.command is the engine command (reasonrd by default).
Flags are passed to the engine as is; without paths, the project sources.

The engine checks engine.yaml and builds a server with its own adapters:
reasonrd check, reasonrd build.

The default path is the project sources (src from reasonr.yaml); outside a project,
the current directory.
Exit code: 0 no errors, 1 errors, 2 failure.
`

func main() {
	// REASONR_CPUPROFILE is a CPU profile of the command for measurements (docs/plans/0015).
	if path := os.Getenv("REASONR_CPUPROFILE"); path != "" {
		if f, err := os.Create(path); err == nil {
			_ = pprof.StartCPUProfile(f)
			code := run(os.Args[1:], os.Stdout, os.Stderr)
			pprof.StopCPUProfile()
			f.Close()
			os.Exit(code)
		}
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stderr, usage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "check":
		return checkCmd(args[1:], stdout, stderr)
	case "test", "dev":
		return launchCmd(args[0], args[1:], stdout, stderr)
	case "fmt":
		return fmtCmd(args[1:], stdout, stderr)
	case "build":
		return buildCmd(args[1:], stdout, stderr)
	case "explain":
		return explainCmd(args[1:], stdout, stderr)
	case "graph":
		return graphCmd(args[1:], stdout, stderr)
	case "extract":
		return extractCmd(args[1:], stdout, stderr)
	case "api":
		return apiCmd(args[1:], stdout, stderr)
	case "impact":
		return impactCmd(args[1:], stdout, stderr)
	case "pii":
		return piiCmd(args[1:], stdout, stderr)
	case "feature":
		return featureCmd(args[1:], stdout, stderr)
	case "diag":
		return diagCmd(args[1:], stdout, stderr)
	case "lsp":
		return lspCmd(args[1:], stdout, stderr)
	case "ui":
		return uiCmd(args[1:], stdout, stderr)
	case "mod":
		return modCmd(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "reasonr: unknown command %q\n\n%s", args[0], usage)
	return 2
}

// collect returns a sorted list of *.rr files.
func collect(paths []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			add(root)
			continue
		}
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(p, ".rr") {
				add(p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}
