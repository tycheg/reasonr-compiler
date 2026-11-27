GO ?= go
FUZZTIME ?= 20s
# Two Go modules: the compiler (the root) and docs (the RFC, the roadmap,
# plans; its test writes the RFC blocks into examples/). The example models
# require the auth module by version: make modules puts it into the module
# cache before the tests.
EXAMPLES = examples/taxi examples/bank

.PHONY: all check modules fmt-check vet lint test test-race examples bench fuzz scale update build install vscode

all: check

check: fmt-check vet lint test

modules: build
	for d in $(EXAMPLES); do (cd $$d && $(CURDIR)/bin/reasonr mod download) || exit 1; done

fmt-check:
	@out=$$(gofmt -l . | grep -v node_modules); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...
	cd docs && $(GO) vet ./...

lint:
	staticcheck ./...
	cd docs && staticcheck ./...

test: modules
	$(GO) test ./...
	cd docs && $(GO) test ./...

test-race: modules
	$(GO) test -race ./...
	cd docs && $(GO) test -race ./...

# The examples on the engine: reasonrd of the standard distribution from
# PATH (go install github.com/tycheg/reasonr-dist/cmd/reasonrd@latest).
examples: modules
	for d in $(EXAMPLES); do (cd $$d && $(CURDIR)/bin/reasonr check && reasonrd test src) || exit 1; done

# Benchmarks of the interpreter (the targets are in RFC §29).
bench:
	$(GO) test ./internal/runtime/vm -run='^$$' -bench=. -benchmem

# A short run of the fuzz tests; the nightly run uses FUZZTIME=1h.
fuzz:
	$(GO) test ./internal/lexer -run='^$$' -fuzz=FuzzLexer -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/parser -run='^$$' -fuzz=FuzzParser -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/format -run='^$$' -fuzz=FuzzFormat -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/sema -run='^$$' -fuzz=FuzzCheck -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/check -run='^$$' -fuzz=FuzzAnalyzers -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/lower -run='^$$' -fuzz=FuzzLower -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/ir -run='^$$' -fuzz=FuzzDecode -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/compat -run='^$$' -fuzz=FuzzCompat -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/lsp -run='^$$' -fuzz=FuzzServer -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/runtime/dec -run='^$$' -fuzz=FuzzArith -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/runtime/record -run='^$$' -fuzz=FuzzDecode -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/runtime/jsonv -run='^$$' -fuzz=FuzzDecode -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/runtime/jsonv -run='^$$' -fuzz=FuzzRoundTrip -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/runtime/bytecode -run='^$$' -fuzz=FuzzDecode -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/codegen -run='^$$' -fuzz=FuzzCodegen -fuzztime=$(FUZZTIME)
	$(GO) test ./internal/runtime/vm -run='^$$' -fuzz=FuzzExec -fuzztime=$(FUZZTIME)

# Measurements at scale (docs/plans/0015): a synthetic model of SCALE lines
# (100k, 1m, 10m) in .scale/, the compiler's time and memory by phase with
# pprof profiles and budgets. The previous report is the comparison base: a
# phase 20 % slower is a failure. The engine measures the same model
# (make scale SCALE_MODEL=… in reasonr-engine).
SCALE ?= 1m
SCALE_DIR = $(CURDIR)/.scale
scale:
	mkdir -p $(SCALE_DIR)/model-$(SCALE)
	if [ -f $(SCALE_DIR)/compiler-$(SCALE).json ]; then cp $(SCALE_DIR)/compiler-$(SCALE).json $(SCALE_DIR)/compiler-$(SCALE).prev.json; fi
	$(GO) run ./internal/scale --lines $(SCALE) --model $(SCALE_DIR)/model-$(SCALE) --profile $(SCALE_DIR)/pprof-$(SCALE) --budget \
		$$([ -f $(SCALE_DIR)/compiler-$(SCALE).prev.json ] && echo --baseline $(SCALE_DIR)/compiler-$(SCALE).prev.json) -o $(SCALE_DIR)/compiler-$(SCALE).json

# Update the golden files: the examples from the RFC, then the golden files
# of the packages whose tests import golden (the others have no -update flag).
GOLDEN = $(GO) list -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}'
update: modules
	cd docs && $(GO) test . -update
	$(GO) test $$($(GOLDEN) ./... | grep /internal/golden | cut -d" " -f1) -update

build:
	$(GO) build -o bin/reasonr ./cmd/reasonr

install:
	$(GO) install ./cmd/reasonr

# The VS Code extension: dependencies, the build, the grammar check, the .vsix package.
vscode:
	cd vscode && npm ci && npm run compile && npm test && npm run package
