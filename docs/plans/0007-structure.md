# Plan: repository structure — docs, examples, compiler, engine

| | |
|---|---|
| **Basis** | The request of 2026-11-06: separate the docs, the examples, the compiler and the engine; examples of three parts by role; tests next to what they test; the compiler does not depend on the engine; the engine and its adapters will grow in separate repositories |
| **Status** | Decisions approved 2026-11-06. Stages S1–S7 are done, S8 (tags and publishing) — on command; progress and deviations — [§12](#12-progress). The repository split that followed is [plan 0020](0020-split.md): the paths below are those before it |
| **Date** | 2026-11-06 |

---

## 1. Goal

| Part | What is inside | Who edits it |
|---|---|---|
| `docs/` | The RFC, plans, the sync of examples with the RFC | the language authors |
| `examples/` | Example models, each of three parts (§5) | product engineers, integrators, engine and UI developers — each their own part |
| `compiler/` | The language: the abstraction for engines, the language runtime (VM, bytecode, values), the compiler, the `reasonr` CLI, the LSP, the VS Code extension | compiler developers |
| `engine/` | The default engine: ports, the kernel, adapters, test mode, the test runner, the web runtime and the primitive catalog, `reasonrd`, `reasonr-web` | engine and UI developers |

The root holds only `README.md`, `AGENTS.md`, `LICENSE`, `Makefile`.

Dependencies go one way: **examples → engine → compiler**. The compiler knows nothing of the engine; the engine implements the compiler's abstraction. An example is three directories that can be copied anywhere and work on the installed tools and the published module versions (§6).

## 2. Decisions

| # | Decision | Status |
|---|---|---|
| 1 | **The abstraction and the language runtime are in the compiler.** The `reasonr` package (the schema, values, the UI program, the unit of work, the `Interpreter` and `Env` interfaces) is in `compiler/reasonr`; the VM, bytecode, records, decimals and the canonical JSON of values are in `compiler/runtime/`. Bytecode is the compiler's output, and the VM runs it with the language semantics; the engine implements `Env` (entities, index searches, time, the caller) and gets the interpreter from the compiler | approved |
| 2 | **Commands that need the engine are in the engine.** `reasonr test` → `reasonrd test`, `reasonr engine check` → `reasonrd check`, `reasonr engine build` → `reasonrd build`, `reasonr build --integrations-openapi` → `reasonrd openapi --integrations`. A custom engine build (`taxi-engine test`) runs the tests on the engine the model runs on. Building a program from a model (`testrun.Build`) goes to the compiler: `compiler.Build` | approved |
| 3 | **Models for tests.** Compiler tests build models from the RFC blocks into a temporary directory (the compiler implements the RFC). The engine has its own models in `engine/testdata/`: it will move to a separate repository; while it is here, the `docs` sync updates them | approved |
| 4 | **Go modules without a root.** `compiler/go.mod` (`github.com/tycheg/reasonr/compiler`), `engine/go.mod` (`…/engine`), separate modules for adapters with third-party libraries and for `reasonrd`, modules of the example parts, `docs/go.mod`. Locally the modules are linked by `go.work` in `engine/` and in `examples/`, not by `replace`: a module with `replace` cannot be installed with `go install …@version` | approved |
| 5 | **The bank has three non-empty parts**: the model in RFC §26 gets an external "antifraud" system (scoring a transfer, the answer as a command) and an app with screens | approved |
| 6 | **The platform ships artifacts, not code**: the `taxi-engine` image is its own `reasonrd` with adapters and a UI build with its own primitives. A product engineer runs the model and the tests in that image with a local configuration (memory, fakes); without the image — the standard `reasonrd`, and the automatic UI draws its own primitives (RFC §22.1) | approved |
| 7 | **Every adapter is in `engine/adapters/<technology>`** (§4.2): `edge/*`, `telemetry/`, the `engine/memory` adapters and the current `adapters/*`. Adapters on the standard library are packages of the `engine` module — the standard `reasonrd` set; those with third-party libraries are separate modules | approved |
| 8 | **The example sync** (`examples/examples_test.go`) moves to `docs/`: RFC blocks with a path header (`# taxi/order.rr`) are written to `examples/taxi/model/…` and to `engine/testdata/taxi/…`; the `integrations/` and `platform/` parts are not in the RFC | approved |
| 9 | Commands: `compiler/cmd/reasonr`, `engine/cmd/reasonrd`, `engine/testrun/web/cmd/reasonr-web` | approved |
| 10 | Fakes are in `model/`: the compiler takes `*.fake.rr` from the module directory; without them the model cannot run without integrations | approved |
| 11 | The example parts are `model/`, `integrations/`, `platform/` | approved |
| 12 | Tests are next to what they test (§7) | approved |

## 3. Compiler

```
compiler/
  go.mod                    github.com/tycheg/reasonr/compiler — no dependencies on the engine
  reasonr/                  the abstraction for engines: Schema, Value, UI, UnitOfWork, Interpreter, Env
  runtime/                  bytecode/ dec/ jsonv/ record/ vm/
  ast/ check/ codegen/ compat/ constant/ diag/ format/ ir/ lexer/ lower/
  module/ parser/ schemagen/ sema/ source/ std/ token/ tools/ types/ uigen/
  build.go                  compiler.Build: model → program (schema, bytecode, UI program)
  internal/                 golden/ rfcblocks/ wanttest/ — test helpers
  lsp/
  editors/vscode/
  cmd/reasonr/              check, fmt, build, explain, graph, pii, diag, ui, lsp
```

The boundary check is a test in `compiler/`: no package of the module imports `github.com/tycheg/reasonr/engine/…` (`go.mod` will not have such a dependency either — the build would fail by itself).

## 4. Engine

### 4.1. Structure

```
engine/
  go.mod                    github.com/tycheg/reasonr/engine → compiler
  go.work                   . ../compiler ./adapters/{postgres,mongodb,kafka} ./cmd/reasonrd ./testrun/web
  port/                     ports: stores, the bus, integrations, the hub, KMS, ingress, authentication, telemetry
  kernel/ dispatch/ stateless/ storekit/ pii/ bus/ config/ registry/ builtin/ check/ uiview/ testapi/
  conformance/              port conformance tests for each adapter
  adapters/                 §4.2
  testrun/                  the scenario test runner: the engine in memory and over HTTP; web/ — the reasonr-web module
  web/                      @reasonr/runtime, @reasonr/std-ui, @reasonr/vite, the built-in bundle
  testdata/                 the taxi and bank models for engine tests
  cmd/reasonrd/             a separate module: serve, migrate, test, check, build, openapi, pii, topics, …
```

### 4.2. Adapters (decision 7)

| Adapter | Ports | From | Module |
|---|---|---|---|
| `adapters/memory` | every store, the hub, the bus, the `memory` KMS, the request queue, coordination | the `engine/memory` adapters; the engine build for tests (`engine.go`, `fakes.go`) goes to `testrun` | `engine` |
| `adapters/http` | `Listener` (the model API, the app API, serving the UI), `IntegrationTransport` | `edge/httpapi` → `http/api`, `edge/appapi` → `http/app`, `edge/webui` → `http/webui`, `edge/integration` → `http/integration` | `engine` |
| `adapters/auth` | `Authenticator`: `jwt`, `keys`, the test login | `edge/auth` | `engine` |
| `adapters/vault` | `KMS` | `edge/vault` | `engine` |
| `adapters/telemetry` | `Metrics`, `Tracer`: `slog`, Prometheus | `telemetry/` | `engine` |
| `adapters/postgres`, `mongodb`, `kafka` | as now | `adapters/*` | their own modules |

`builtin` registers the standard `reasonrd` set; `reasonrd build -f components.yaml` adds adapters from other modules and repositories.

## 5. Examples

```
examples/
  go.work                   the example parts + ../compiler ../engine (not in a copy of an example)
  taxi/
    README.md               roles, parts, running each
    model/                  product engineers
    integrations/           integrators
    platform/               engine and UI developers
  bank/
    model/ integrations/ platform/
```

### 5.1. `model/` — product engineers

- a ReasonR module: entities, commands, queries, apps and screens on primitives, declarations of its own primitives, texts, tests, fakes, tenant extensions;
- `engine.local.yaml`: everything in memory, `integrations: { default: { type: fake } }`;
- `platform.version` — the platform image version;
- `README.md`:

```sh
reasonr check --all-tenants .
docker run --rm -v $PWD:/model taxi-engine:$(cat platform.version) test --all-tenants /model
docker run --rm -p 8080:8080 -v $PWD:/model taxi-engine:$(cat platform.version) \
  --config /model/engine.local.yaml --model /model --test          # http://localhost:8080/ui/
```

Without the image — the same with the standard `reasonrd`.

### 5.2. `integrations/` — integrators

- A Go module of the service: the taxi — `Matching.findDriver`, `Payments.charge`; the bank — antifraud. It accepts `POST /requests/{System}/{request}` (the contract is `reasonrd openapi --integrations`), checks the engine's JWT against `/.well-known/jwks.json`, and answers with commands of the model API with the system's key. The logic is simple: the taxi's drivers come from the service configuration, the first free one not in `exclude`; the bank — an amount threshold;
- the service `Dockerfile`;
- a test: the engine in memory plus the service — the scenario passes without fakes.

### 5.3. `platform/` — engine and UI developers

- `engine/`: `components.yaml` and an adapter of its own — the taxi has a `file` KMS (the master key from a file, like a Docker or Kubernetes secret), the bank a `jsonl` bus (events to a file for audit); `reasonrd build -f components.yaml -o taxi-engine`. The test builds and runs it with its own adapter;
- `ui/web/`: its own primitive implementations through `@reasonr/vite` — the taxi's `Tag` (the order status by color), the bank's by the screens of decision 5. The test is Vitest and a type check by `reasonr build --ui-types`;
- the image `Dockerfile`: the engine with its own adapters and the UI build;
- `engine.yaml` — the production configuration: PostgreSQL, its own adapter, its own UI build, integrations over HTTP.

## 6. Versions and a copy of an example

A copy of the three example directories builds outside the repository once published versions exist:

- **Go**: the tags `compiler/vX.Y.Z` and `engine/vX.Y.Z` (and tags of the adapter modules, `reasonrd`); the `integrations/` and `platform/` parts require those versions in `go.mod`. In the repository the local code comes through `examples/go.work` instead of versions; a copy does not take it;
- **npm**: the packages `@reasonr/runtime`, `@reasonr/std-ui`, `@reasonr/vite` — for `platform/ui/web`;
- **tools**: `go install github.com/tycheg/reasonr/compiler/cmd/reasonr@vX.Y.Z` and `…/engine/cmd/reasonrd@vX.Y.Z`.

Tags and package publishing are outward actions: only on your command (stage S8). Until then a copy is checked with `GOWORK` pointing at a clone.

## 7. Tests next to what they test

| What | Where the tests are |
|---|---|
| Compiler and language runtime packages | in their packages; models from the RFC |
| The LSP, the VS Code extension | `compiler/lsp`, `compiler/editors/vscode/test` |
| Engine packages and adapters | in their packages; models — `engine/testdata`; ports — `engine/conformance` |
| The web runtime | `engine/web/packages/runtime/test` (now `web/test`); screen description golden files — `engine/uiview/testdata` |
| The server, tests through the UI | `engine/cmd/reasonrd/server`, `engine/testrun/web` |
| The sync of examples with the RFC | `docs/` |
| Example parts | in their parts |

## 8. What changes besides directories

- Import paths, `go.mod`, `go.work`; the `Makefile` (targets enter the modules), CI.
- `vite.config.ts` of the built-in bundle: `outDir` → `engine/adapters/http/webui/dist`.
- The RFC (§27, §31, §33), README, AGENTS.md: `reasonrd test`, `reasonrd check`, `reasonrd build`, `reasonrd openapi`; install paths; links `plans/…` → `docs/plans/…`.
- Golden files with paths (`-update`); the agent's memory of project paths.

## 9. Stages

Branch `structure`, a commit per stage; after each — all tests of all modules, Vitest, the VS Code grammar, the taxi through the UI.

| Stage | What |
|---|---|
| S1 | `docs/`: the RFC, plans, the sync module |
| S2 | `compiler/`: the abstraction, the runtime, `compiler.Build`, the LSP, VS Code, the CLI without engine commands; test models from the RFC |
| S3 | `engine/`: adapters (decision 7), the test runner, web, `reasonrd test/check/build/openapi`, `engine/testdata` |
| S4 | `examples/*/model/`: the move, `engine.local.yaml`, README; RFC, README, AGENTS.md — the new commands |
| S5 | The bank in RFC §26: antifraud, the app and screens; tests |
| S6 | `integrations/` of the taxi and the bank with tests |
| S7 | `platform/` of the taxi and the bank: adapters, primitives, images, tests |
| S8 | On your command: version tags, publishing the npm packages; checking a copy of an example outside the repository |

## 10. Done when

1. The root has only `README.md`, `AGENTS.md`, `LICENSE`, `Makefile` and the directories `docs/`, `examples/`, `compiler/`, `engine/`.
2. `compiler/go.mod` does not depend on `engine`; `engine` depends only on `compiler`; the examples on `compiler` and `engine` by version.
3. Every test passes; the PostgreSQL tests with `REASONR_TEST_POSTGRES`.
4. The taxi and the bank have three non-empty parts, each with a passing test.
5. A copy of `examples/taxi` in `/tmp` (before S8 — with `GOWORK` on a clone): `reasonr check`, the tests in the platform image, a run with the UI; the integration test without fakes.
6. Files are under 1000 lines; `git log --follow` shows the history of moved files.

## 11. Risks

| Risk | What we do |
|---|---|
| Splitting `engine/memory` into adapters and the test engine creates a package cycle | First move it whole into `adapters/memory`, split in a separate commit if the split is clean |
| `Env` and `Interpreter` turn out insufficient: the engine reaches into VM or bytecode internals (`storekit` takes `bytecode.IDField`) | What the engine needs goes into `compiler/reasonr`; whatever stays in `runtime/` the engine imports only through the public API |
| External links and install commands break | The module has no tags and no external users; README and AGENTS.md get the new paths |
| The platform image needs Docker in tests | The platform test builds the binary and runs it without Docker; building the image is a separate CI step |

## 12. Progress

| Stage | State |
|---|---|
| S1 | Done: `docs/rfc`, `docs/plans`, the `docs` module with the example sync |
| S2 | Done: the `compiler` module (the abstraction `compiler/reasonr`, the runtime `compiler/runtime`, `compiler.Build`, the LSP, VS Code, the CLI); `reasonrd test`, `check`, `build`, `openapi --integrations`; `TestNoEngineImports` |
| S3 | Done: the `engine` module, adapters in `engine/adapters`, `engine/go.work`, test models `engine/testdata`, the web runtime tests in `packages/runtime/test` |
| S4 | Done: `examples/*/model`, `engine.local.yaml`, `reasonrd check --test` |
| S5 | Done: the bank in RFC §26 — antifraud (`Antifraud.score`, the answer `Freeze`), the `Bank` app, its own primitive `AccountNumber` |
| S6 | Done: `examples/*/integrations` — services on the standard library, tests on `reasonrd` without fakes |
| S7 | Done: `examples/*/platform` — the `webhook` and `jsonl` buses, `Tag` and `AccountNumber` through `@reasonr/vite`, configurations, `Dockerfile` |
| S8 | On command: the tags `compiler/vX.Y.Z`, `engine/vX.Y.Z` (and of the adapter modules, `reasonrd`), the `@reasonr/*` packages on npm, versions in `go.mod` and `package.json` of the example platforms. Plan 0020 replaces it with per-repository tags (phase C) |

**A copy of an example outside the repository.** `model/` and `integrations/` work now: they need only `reasonr`, `reasonrd` and Go. `platform/` builds after S8: `reasonrd build` and `npm ci` take `github.com/tycheg/reasonr/…` and `@reasonr/*` by version. If the repository is private, Go needs `GOPRIVATE=github.com/tycheg/*` and access to GitHub.

Deviations from the plan:

1. **The taxi's adapter is a `webhook` bus, not a `file` KMS.** The `local` KMS already reads the master key from a file (`key_file`), while the engine had no bus to HTTP.
2. **Compiler tests read the RFC through the link `compiler/testdata/rfc.md`.** `go test` does not recheck files outside the module: an RFC edit returned cached results.
3. **The tests of the example parts run with `-count=1`** (`make test`): they read `../model` outside their module.
4. **`replace` for `v0.0.0` is in `go.work`.** Go reads the `go.mod` of every requirement, even a workspace module; the modules' `go.mod` files have no `replace`.
5. **`engine.example.yaml` and `engine.postgres.yaml` stayed in `engine/cmd/reasonrd`** — they are engine configuration examples, and the server test checks them. The examples have their own: `model/engine.local.yaml` (the standard `reasonrd`), `integrations/engine.yaml`, `platform/engine.yaml` and `platform/engine.local.yaml` (the image).
6. **The bank's scenarios on the fake are a separate file `antifraud.test.rr`**, like the taxi's `ride.test.rr`: the `lower` fuzz test seed is one file without fakes.
7. **The platform UIs take `@reasonr/*` as copies** (`file:` and `install-links`) so that the bundle has one React; the plugin is included as source.
8. **Found and fixed:** `reasonr build --ui-types` and `--dump-ui` appended the IR hash; `@reasonr/vite` took `*.test.tsx` into the bundle and the manifest.
