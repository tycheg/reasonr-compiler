# ReasonR compiler

ReasonR is a language for business logic in the spirit of DDD, CQRS and Event Sourcing: product engineers write the model together with AI agents, and the engine takes care of storage, the API and the UI.

This repository is the language: the specification, the `reasonr` compiler and its tools, the language server and the VS Code extension, the bytecode interpreter, the standard UI catalog declarations and the example models.

- The language specification — [RFC-0001](docs/rfc/0001-reasonr-language/README.md).
- The roadmap — [docs/roadmap.md](docs/roadmap.md); active plans — [docs/plans](docs/plans).
- Example models — [examples/](examples/): taxi (with the `acme` extension) and a bank transfer.
- Rules for AI agents working on a model, the JSON formats of diagnostics and test reports — [AGENTS.md](AGENTS.md).

The other parts of ReasonR:

| Repository | What it is |
|---|---|
| [reasonr-engine](https://github.com/tycheg/reasonr-engine) | The engine core and the contract of plugins: ports, kernel, built-in adapters, test drivers, the server, the builder `reasonr-build` |
| [reasonr-stateless](https://github.com/tycheg/reasonr-stateless) | The stateless engine kind (the default): transactions in the database |
| [reasonr-postgres](https://github.com/tycheg/reasonr-postgres), [reasonr-mongodb](https://github.com/tycheg/reasonr-mongodb), [reasonr-kafka](https://github.com/tycheg/reasonr-kafka), [reasonr-vault](https://github.com/tycheg/reasonr-vault) | Store, bus and KMS adapters |
| [reasonr-web](https://github.com/tycheg/reasonr-web) | The React runtime, the standard UI catalog, the `reasonr-ui` build tool and the web test driver |
| [reasonr-dist](https://github.com/tycheg/reasonr-dist) | The standard `reasonrd`: the engine with the official plugins |
| [reasonr-auth](https://github.com/tycheg/reasonr-auth) | The authentication module: the `Auth` model; Keycloak tokens go through the engine's `jwt` settings |

## Install

Go 1.23+.

```bash
go install github.com/tycheg/reasonr-compiler/cmd/reasonr@latest
go install github.com/tycheg/reasonr-dist/cmd/reasonrd@latest   # the engine: tests, local runs
```

## Tools

`reasonr` commands are the compiler: they need no engine. Acceptance tests, `engine.yaml` checks are done by the engine: `reasonrd test`, `reasonrd check`; a server with its own adapters is built from a manifest by `reasonr-build` (`reasonr-engine/cmd/reasonr-build`).

```bash
reasonr check examples                          # every module in the directory: base models and models with extensions
reasonr check --extension base examples         # base models only (--extension acme — only acme)
reasonr check --format json examples            # diagnostics for agents and the LSP (AGENTS.md)
reasonr fmt -w examples/taxi/src                # format the files (--check — for CI)
reasonr build --extension acme --dump-ir examples/taxi/src     # the IR of a model with an extension
reasonr build --dump-schema examples/bank/src > bank.json      # the model schema
reasonr build --dump-bytecode examples/bank/src                # the bytecode the interpreter runs
reasonr check --against bank.json examples/bank/src            # compatibility with the previous version
reasonr impact --base origin/main examples/taxi/src            # which tests a change affects (RFC §27.4)
reasonr explain Order.Cancel examples/taxi/src                 # what a command does
reasonr graph --entity Order examples/taxi/src                 # a Mermaid diagram
reasonr graph --contexts examples/taxi/src                     # the context map: ACLs between bounded contexts
reasonr extract Dispatch examples/taxi/src                     # what changes if Dispatch becomes a separate process
reasonr api list examples/taxi/src                             # the external API contract: what is published, versions
reasonr api openapi --version 2026-11-13 examples/taxi/src     # OpenAPI as the version's clients see it
reasonr pii examples/taxi/src                                  # the personal data map
reasonr feature list examples/taxi/src                         # feature flags and the tests that switch them on
reasonr build --dump-ui examples/taxi/src                      # the interface program: screens, places, types
reasonr build --ui-types examples/taxi/src                     # TypeScript props for primitive implementations
reasonr ui eject <App> <module>                                 # an `app … auto`: the layout it builds, as source
reasonr diag explain E0504                                     # an explanation of a code with an example
reasonr diag list                                              # every diagnostic code
reasonr lsp                                                    # the language server (stdin/stdout)
```

With the engine installed:

```bash
reasonrd test examples/taxi/src examples/bank/src              # acceptance tests on the in-memory engine
reasonrd test -v --run "unique" examples/taxi/src              # steps, notifications, requests
reasonrd test examples/taxi/src/contexts/Ordering/order.test.rr:30   # one test: the one containing line 30
reasonrd dev examples/taxi/src                                 # the model in memory in test mode: API and UI at :8080
```

## A model project: `reasonr test` and `reasonr dev`

`reasonr.yaml` in a model directory sets the sources (`src`), the required modules and the engine the model runs on: a platform image (`platform.image`), the platform sources to build it (`platform.build`), or a command (`platform.command`, `reasonrd` by default). `reasonr test` and `reasonr dev` run it — as a command or through `docker`; flags go to the engine as is, without paths — the project sources. Compiler commands (`reasonr check`, `fmt`, `build`, …) without a path in a project take `src`.

```bash
reasonr mod download                  # the modules of requires into the module cache and reasonr.lock
reasonr check && reasonr test         # every model (base and extensions), the tests on the project's engine
reasonr test src/contexts/Ordering/order.test.rr:30
reasonr test --driver api,web         # a pipeline: a run through the API and runs through the web places
reasonr dev                           # the platform's debug environment: databases and the engine in test mode
```

## Development

Go 1.23+; Node.js 20+ for the VS Code extension.

```bash
make modules                          # the modules the example models require (reasonr mod download): before the tests
make check                            # gofmt, vet, staticcheck, the tests of the compiler and of docs
make examples                         # reasonr check and the acceptance tests of the examples on reasonrd from PATH
make update                           # rewrite the example models from the RFC blocks (the RFC is the source of truth), then the golden files
make fuzz FUZZTIME=1m                 # fuzz tests
make bench                            # interpreter benchmarks
make vscode                           # build the VS Code extension (.vsix)
make install                          # reasonr into $(go env GOPATH)/bin
```

A release is a tag `vX.Y.Z` (and `docs/vX.Y.Z` for the `docs` module).

## Contracts

The only Go contract is the module root, package `reasonr` (`TestOnePublicPackage` checks it): `Build`, `Load`, `Open` a model built by `reasonr build -o`, the model object `Model` with its methods, the boundary types (type aliases of `internal/model`), the callbacks the engine passes in (`Env`, `Driver`, `ValueWriter`, `ValueReader`). Only the engine imports it (`scripts/deps-check.sh` of the engine side). The language, `reasonr.yaml`, the CLI and its JSON are the product's external surface (the RFC).

## Layout

| Directory | What is in it |
|---|---|
| `.` (package `reasonr`) | The only public package: the compiler's contract with the engine — `Build`, `Load`, `Open`, the model object `Model` and the boundary types (RFC §30) |
| `internal/model` | The boundary types: the model schema (RFC §30.5), values, the arena, calls, the unit of work, `Interpreter` and `Env` (RFC §30.1–§30.4), the interface program — apps, screens, nodes, places, types, texts |
| `internal/runtime/dec` | `decimal`: 128 bits, 18 digits after the point, rounding (RFC §7.1) |
| `internal/runtime/record` | The binary encoding of records by schema, default values |
| `internal/runtime/jsonv` | Canonical JSON of values by schema |
| `internal/runtime/bytecode` | The bytecode format: instructions, call and search tables, serialization, disassembler, verifier |
| `internal/runtime/vm` | The bytecode interpreter: commands and the unit of work, queries, rules, methods, the standard library, migrations, acceptance test steps and checks |
| `internal/pipeline` | The common entry of the CLI, the LSP and the tools: loading a module, building a model with an extension, every check |
| `internal/source` | Files, positions, lines and columns |
| `internal/diag` | Diagnostics: the code registry, messages, text and JSON output, code explanations with examples (`explain/*.md`) |
| `internal/token`, `internal/lexer` | Tokens, indentation → INDENT/DEDENT |
| `internal/ast`, `internal/parser` | The syntax tree and the parser with error recovery |
| `internal/format` | The `reasonr fmt` formatter |
| `internal/module`, `internal/project` | Loading a module by project parts (bounded contexts, apps, primitives), extensions; `reasonr.yaml` and modules |
| `internal/types`, `internal/sema` | Model types and symbols; name and access resolution, type checking of bodies, flow narrowing, bounded contexts |
| `internal/constant` | Constant expression evaluation: default values |
| `internal/check` | Model checks: the call graph, indexes and queries, uniqueness, access, personal data, warnings |
| `internal/ir`, `internal/lower` | A linear typed IR and lowering the checked model into it |
| `internal/codegen` | IR + schema → bytecode |
| `internal/schemagen` | The model schema: types, fields, indexes, methods, access, personal data, defaults |
| `internal/compat`, `internal/apicontract`, `internal/apidoc` | Model version compatibility (`--against`), the external API contract, OpenAPI and MCP descriptions |
| `internal/impact` | Test impact analysis (`reasonr impact`) |
| `internal/std` | The standard catalog of UI primitives `ui.rr`, part of every module |
| `internal/uigen`, `internal/uiview` | The interface program from the model, the automatic UI, TypeScript types (`--ui-types`); screen descriptions and app manifests for platform runtimes |
| `internal/tools` | `explain`, `graph`, `pii`, `extract` |
| `internal/lsp` | The language server: its own JSON-RPC layer, a workspace with open documents and a parse cache |
| `internal/golden`, `internal/wanttest`, `internal/rfcblocks`, `internal/genmodel`, `internal/scale` | Test helpers: golden files, `# want "CODE"` expectations, RFC blocks and the example models from them, synthetic models and scale measurements |
| `cmd/reasonr` | The compiler CLI |
| `vscode` | The VS Code extension: a TextMate grammar, an LSP client, the Format and Explain command commands |
| `docs` | The RFC, the roadmap and plans (Go module: its test writes the RFC blocks into `examples/`) |
| `examples` | The RFC example models: taxi and bank |
