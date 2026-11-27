# Plan: low coupling, high cohesion — contracts and the repository structure

| | |
|---|---|
| **Basis** | [Roadmap](../roadmap.md), item 11; the author's requirements of 2026-11-26: the contract between repositories is as small as possible; functionality moves to the side whose reason to change it is; one engine with one contract and its implementations (engine kinds) |
| **Status** | Agreed 2026-11-26 (§2). Done before phase C of [plan 0020](0020-split.md): the copy publishes this structure |
| **Branch** | `plan-0021` |

---

## 1. Goal

1. **Small Go contracts.** Between our Go modules there are four packages and a conformance test kit. Everything else is internal and changes in any release.
2. **Only the engine calls the compiler.** Adapters, engine implementations and test drivers see the engine's contract only.
3. **One engine, several implementations.** The engine repository holds the contract, the shared part and the host; the engine kinds (stateless and others) are implementations in their own repositories.
4. **Cohesion by the reason to change.** Everything that understands the language is in the compiler; storage, transport, execution and serialization formats are in the engine; a technology is in its adapter.
5. **Before phase C.** `v0.1.0` freezes only these contracts.

## 2. Decisions

| # | Decision |
|---|---|
| 1 | **The compiler has one public package**, the module root (`github.com/tycheg/reasonr-compiler`, package `reasonr`): building a model (`Build` from sources, `Open` from a file built by `reasonr build -o`) and the model as an object with methods. The boundary types live in an internal package and are exported by type aliases: one package without an import cycle |
| 2 | **The model is an object, not data.** The engine calls `Model.Execute`, `Query`, `Migrate`, `Fold`, `Scenarios`, `RunScenario`, `PerfCheck`, `Manifest`, `Screen`, `ScreenData`, `OpenAPI`, `Tools`, `Version`, `Compatible`, and `Schema.Read`/`Write` for values. The VM, the bytecode, the UI program, the API views, the syntax tree, the test index and the diagnostics types are internal; model errors come rendered (text and the JSON of AGENTS.md) |
| 3 | **Everything that understands the language moves into the compiler.** Finding tests and scenarios (today the engine walks `ast.TestDecl`), screen descriptions (`engine/uiview`), walking a screen with its conditions (the app API), OpenAPI and MCP schemas, the API version views, the compatibility check, building programs for the conformance suites |
| 4 | **Only the engine calls the compiler.** Engine implementations get command and query arguments encoded (a kind with a command journal stores them as they are; the kernel decodes); test drivers get steps with arguments in canonical JSON; store adapters get bytes and plain Go values (decision 5) |
| 5 | **Store adapters work with bytes.** The kernel encodes the state, computes the index key columns (plain Go values: `int64`, `string`, `bool`, `[]byte`, decimal strings), encrypts personal fields and builds blind keys, builds the outbox record; the store writes, reads and searches by key columns and reports the violated unique index by name. The data layout in the databases does not change: the same `state`, key columns and `sealed`; no data migration |
| 6 | **Serialization is an engine port with adapters** (`port.Codec`, `registry.RegisterCodec`). The built-in codec is `json` (canonical JSON, as today); others are adapter modules. A codec is chosen per place in `engine.yaml` (ingress by `Content-Type`/`Accept`, bus, integrations); a store declares the state format it accepts (PostgreSQL `jsonb` — json only). The model walks a value by its schema and calls the codec's `Writer`; the compiler declares the same method sets, so a codec adapter imports only `port`. Schema formats (Protobuf, Avro) get stable field numbers from `Schema.Describe` |
| 7 | **The engine kind contract** (`port.Kind`, `port.Engine`, `port.Host`): a kind opens an engine — an executor, background roles, `Close`; the host builds the ingress, authentication, notifications, the test API and telemetry around any kind; a kind gets the shared part as interfaces from `port.Host`: tenant models, a kernel over a given state store, an in-memory store, the effect stream, feature flags, keys, telemetry. No shared packages are exported |
| 8 | **The stateless engine is its own repository** (`reasonr-stateless`), like any other engine kind. The engine's own end-to-end tests use a small in-memory test kind of `porttest` |
| 9 | **Adapters see `port` and `registry` only.** Bus messages come with CloudEvents headers and body ready; integration requests with body and headers ready; the hub only carries bytes between instances, the engine fans out to local subscribers; a factory gets its settings node and `Deps` of interfaces |
| 10 | **Keycloak is configuration.** The Go module `reasonr-auth/keycloak` is removed: it only presets the built-in `jwt` (issuer, JWKS, actor `Auth.User`, roles from `realm_access.roles`), which already supports all of it. `reasonr-auth` is a ReasonR module without Go code; its README gives the `jwt` settings for Keycloak |
| 11 | **The Vault KMS is its own repository** (`reasonr-vault`) |
| 12 | **The web test driver is an engine adapter** (`registry.RegisterDriver`): `reasonrd test --driver web` in a distribution that links it; the `reasonr-web` program and a public test runner go away. The web bundle registers itself with `registry.RegisterUIBundle` |
| 13 | **The VS Code extension** moves to `vscode/` at the root of the compiler repository: it uses no Go package, only `reasonr lsp`, and its grammar changes together with the syntax |
| 14 | **CloudEvents** keeps its name (the bus format is the CNCF standard) |
| 15 | **Enforcement.** `scripts/deps-check.sh` becomes an allowlist (§5); `internal/` enforces the rest. Each repository's README gets the sections "Contracts" and "Layout" |

## 3. Today

Measured on 2026-11-26 by `go list` over every module (production code and tests):

| Boundary | Today | Problem |
|---|---|---|
| The compiler's public surface | 39 packages, 6 of them internal | Every compiler package is a contract: a refactoring of the parser is a breaking change |
| Compiler → engine | The engine imports 19 compiler packages: the facade, `reasonr`, `runtime/*`, `diag`, `source`, and also `ast` (`server/demo.go`, `testrun`, `perf` walk the syntax tree to find tests), `lower`, `codegen`, `schemagen` (`conformance`, `perf` build programs by hand), `module` (`testrun` finds modules), `uigen` (`server` builds the UI program), `compat`, `apicontract`, `apidoc` | The engine interprets the language's text itself; any change of the syntax tree or the pipeline breaks it |
| Compiler → adapters | The PostgreSQL adapter uses 68 distinct names of the compiler package `reasonr`, MongoDB 65: values, arenas, schemas, search specs | Changing how the model represents values breaks every store |
| Compiler → web | The web driver imports `uigen` to build the UI program, `vm`, `bytecode`, `reasonr` | The same |
| The engine's public surface | 31 packages, 2 of them internal; other modules import 24 | There is no line between what adapters may use and what is the engine's own code |
| Engine → another engine kind | A kind of another module imports 16 engine packages, 12 in production code: `kernel`, `dispatch`, `features`, `transfer`, `stateless`, `adapters/memory`, `adapters/telemetry`, `server`, `config`, `pii`, `port`, `storekit`. It builds a stateless engine (`stateless.Build`) and replaces its executor | The kind depends on the stateless implementation |
| Engine → adapters | `port`, `registry`, `config`, `bus`, `storekit`, `pii`, `conformance`, and two leaks: `postgres/hub.go` uses `adapters/memory` (`memory.Hub`), `kafka/requests.go` uses `adapters/http/integration` (`integration.Body`, `integration.Key`) | Adapters depend on other adapters |
| Engine → web | The bundle calls `appapi.SetDefaultBundle`; the web driver imports `server`, `testrun`, `testmodels` | The bundle depends on an HTTP adapter of the engine |
| Engine → auth | `keycloak` imports `adapters/auth` (JWT verification), `config`, `port`, `registry` | The same |
| Serialization | `port.Codec` is declared but neither implemented nor called; canonical JSON (`runtime/jsonv`) is called directly from 20 places | No way to choose a format; the compiler's encoding is a contract of every store |

## 4. Go contracts (target)

| Package | Owner | Imported by | What |
|---|---|---|---|
| `github.com/tycheg/reasonr-compiler` (`reasonr`) | compiler | engine only | `Build`, `Open`, `Model` and its methods, boundary types (aliases), `Schema.Read`/`Write`/`Describe`, the callback interfaces `Env`, `Driver`, `ValueWriter`, `ValueReader` |
| `github.com/tycheg/reasonr-engine/port` | engine | adapters, engine kinds, the web module, platforms' own adapters | Ports: stores, projections, events, timers, the effect stream, coordination, the request queue, keys, schema, bus, integration transport, authentication, KMS, hub, codec, test driver, UI bundle; the executor; the engine kind and its host |
| `github.com/tycheg/reasonr-engine/registry` | engine | the same | `Register*` by type, the settings node, `Deps` of interfaces, CLI commands of engine kinds |
| `github.com/tycheg/reasonr-engine/server` | engine | the `reasonrd` distribution, platforms | `Main`, `Run` |
| `github.com/tycheg/reasonr-engine/port/porttest` | engine | tests of adapters and engine kinds | Conformance suites; a small in-memory test kind |

Formats and protocols — the language, the HTTP API, the app API, MCP, the integration contract, `engine.yaml`, the bus format, screen descriptions, `reasonr.yaml` and `reasonr.lock`, the CLI and its JSON — are the product's external surface and are documented where they are today; they are not Go contracts between our modules.

## 5. Who calls whom

| Direction | Calls | On the hot path |
|---|---|---|
| engine → compiler | `Build`/`Open`; `Model.Execute`, `Query`, `ScreenData`, `Schema.Read`/`Write`; the rest once per model version or for tests | every command, query, screen |
| compiler → engine | callbacks passed in: `env.Load`/`Find`/`NewID`, test steps, codec `Writer`/`Reader` methods | inside every command and query |
| engine → engine kind | `Kind.Open`; `Executor.Command`/`Query`/`Subscribe`; `Role.Run`; `Close` | every command and query |
| engine kind → engine | `Host.Model`, `Host.Kernel(store)` → `Kernel.Command`/`Query`/`Fire`, `Host.MemoryStore`, `Host.Effects`, `Host.Features`, `Host.Keys`, telemetry; `registry.RegisterKind`, `RegisterCommand` in `init()` | every command and query (through the kernel) |
| engine → adapter | the ports of §4 | stores and authentication on every request; the rest after commit |
| adapter → engine | `registry.Register*` in `init()`, `Deps` (log, metrics), the hub's delivery callback | — |
| engine ↔ test driver | `registry.RegisterDriver`; `TestDriver.Open`, `Places`; steps through the model's `Driver` callbacks | tests |
| platform → engine | `server.Main`; imports of adapter and kind modules | — |

The allowlist of `deps-check.sh`: the engine may import the compiler package; adapters, engine kinds, the web module and platforms may import `port`, `registry`, `server` (platforms and the distribution) and `porttest` (tests); nothing else crosses a repository.

## 6. Repository structure (target)

```
reasonr-compiler/
  reasonr.go …           the one public package: Build, Open, Model, types (aliases)
  internal/              lexer, parser, ast, format, module, project, sema, types, check, ir, lower,
                         codegen, schemagen, compat, uigen, screens, apicontract, apidoc, impact, tools,
                         lsp, std, vm, bytecode, values (json, record, decimal), model (the boundary types)
  cmd/reasonr/           the CLI
  vscode/                the VS Code extension
  docs/                  RFC, roadmap, plans (its own Go module: the RFC sync)
  examples/              taxi, bank

reasonr-engine/
  port/                  ports, the executor, the engine kind and its host, the codec          public
  port/porttest/         conformance suites, an in-memory test kind                           public
  registry/              registration, settings node, Deps                                     public
  server/                Main, Run                                                              public
  internal/              the host, the kernel, the effect stream, feature flags, keys, the
                         in-memory stores, the HTTP API, the app API, MCP, the test API, the
                         integration transport over HTTP, JWT and API keys, the json codec,
                         the memory/api/mcp test drivers, engine check, performance tests, demo
  cmd/reasonrd/          the distribution (its own module): host + stateless +
                         official adapters + web bundle and driver

reasonr-stateless/       the stateless engine kind: local executor, commit in the main store, timers
reasonr-postgres/, reasonr-mongodb/, reasonr-kafka/, reasonr-vault/   adapters
reasonr-web/             npm packages (runtime, std-ui, vite); Go: bundle (RegisterUIBundle), driver (RegisterDriver)
reasonr-auth/            the ReasonR module Auth (no Go code)
```

## 7. Stages

Each stage is a branch with `make check`, `make install`, `make images` and the documented commands run as a user, as in plan 0020.

| Stage | What | Done when |
|---|---|---|
| L0 | **Full-scale tests of web**, before the restructuring: a platform's own primitive implementation is built against the interfaces generated from the model (`reasonr-ui types`, `typecheck`, `test`, `build`), its manifest passes `engine check` (C0016), the engine serves the build, the web driver goes through the taxi places. Today only the pilot checks this | The web module's tests cover it without the pilot |
| L1 | **The model object.** `Build`, `Open` (`reasonr build -o`), `Model` with the methods of decision 2; scenarios, screens, screen data, API views and schemas, compatibility, rendered errors; `Schema.Read`/`Write` with the writer and reader interfaces. The engine switches to them | The engine imports nothing of the compiler but these |
| L2 | **The compiler's `internal/`.** Every other package moves under `internal/`; the boundary types are aliases in the root package; `editors/vscode` → `vscode/` | The compiler has one public package; every module builds |
| L3 | **Codecs.** `port.Codec`, `registry.RegisterCodec`, the built-in `json`; the 20 direct calls of canonical JSON go through codecs; codec choice in `engine.yaml` | The same reports, API responses and stored data as before; a test codec adapter round-trips the examples |
| L4 | **Stores with bytes.** The kernel encodes rows, keys, encryption, the outbox; the store ports take bytes and plain Go values; PostgreSQL, MongoDB and memory move; the conformance suites | The suites and the examples pass on every store; `TestLoad` within 5 % of today; an existing database works without migration |
| L5 | **Engine kinds.** `port.Kind`, `port.Engine`, `port.Host`; executor requests carry encoded arguments; the host builds the edge around any kind; stateless moves to `stateless/` (its own module, later `reasonr-stateless`); the other kinds move onto `port`; the `porttest` test kind | The engine kinds import only `port`, `registry` and `porttest` (tests) |
| L6 | **Adapters through `port` only.** Ready bus messages and integration requests; the hub without `memory.Hub`; `vault` to `vault/` (its own module, later `reasonr-vault`); keycloak removed in favor of `jwt` settings; the web bundle and driver register through `registry`, `reasonr-web` goes away | Every adapter imports only `port` and `registry` |
| L7 | **The engine's `internal/`.** Everything outside `port`, `port/porttest`, `registry`, `server` | The engine has four public packages |
| L8 | **Enforcement and documentation.** The allowlist in `deps-check.sh`; README "Contracts" and "Layout" in every repository; plan 0020 §1, §3, §4 updated (new repositories `reasonr-stateless`, `reasonr-vault`; `reasonr-auth` without Go; the web driver as an adapter) | `make check` fails on an import outside the allowlist |

Phase C of plan 0020 starts after L8.

## 8. Not in this plan

- **Feature flags of the framework** (compiler, engine, engine implementations, adapters), set at build time or at run time, so that a breaking change in one repository does not stop parallel work in the others — after phase C (roadmap item 31).
- **API diff checks** (`apidiff`/`gorelease`) of the four public packages — together with the framework's feature flags.

## 9. Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| The model object grows into a copy of the compiler's internals | Medium | A method needs an engine caller and returns results, not internal structures |
| Stores with bytes are slower or behave differently | Medium | The conformance suites stay as they are; `TestLoad` is a done-when of L4; the data layout does not change |
| A codec call per field slows the hot path | Low | Measured in L3 on the in-memory engine; the built-in json may keep a direct path |
| `port.Host` grows with every engine feature | Medium | A host method needs both engine kinds as callers |
| Moving to `internal/` touches everything | High | Mechanical moves, one stage per branch, `make check` after each package |
| `v0.1.0` comes later | Certain | The split is published once, with contracts that do not need breaking right away |
