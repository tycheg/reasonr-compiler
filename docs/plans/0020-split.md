# Plan: the repository split

| | |
|---|---|
| **Basis** | [Roadmap](../roadmap.md), item 11; [plan 0007](0007-structure.md), S8 (tags and publishing) |
| **Status** | Approved (2026-11-22): decisions §2.1 and proposals §2.2; answers to the questions — §8. Restructuring and documentation done (§9); copy and publish (§6) in progress since 2026-11-28 (§9) |
| **Branch** | `split-plan` |

This repository becomes several repositories with their own versions, releases, documentation and plans. They start with a clean history; this repository becomes read-only when the stage is done.

---

## 1. Outcome

Framework (owner `github.com/tycheg` for now; open source — §8.2):

| # | Repository | Contents |
|---|---|---|
| 1 | `reasonr-compiler` | language, RFC, `reasonr`, LSP, VS Code extension, interpreter, standard UI catalog declarations (`std/ui.rr`), UI program generator (`uigen`), example models: taxi and bank; AGENTS.md for model authors; framework roadmap |
| 2 | `reasonr-engine` | the contract of plugins `port` — a module of its own without dependencies (`port`, `port/registry`, `port/porttest`, tags `port/vX.Y.Z`, plan 0022); the core with the public packages `kind`, `kind/kindtest`, `server`: the host of engine kinds, kernel, dispatch, personal data, feature flags, config, built-in adapters (memory, http, auth, telemetry), test drivers memory, api and mcp, the server; the builder `reasonr-build` and its image `reasonr-builder` |
| 3 | `reasonr-stateless` | the stateless engine kind (the default): the kernel over the main store, the `timers` role |
| 4 | `reasonr-postgres` | PostgreSQL adapter (every engine kind) |
| 5 | `reasonr-mongodb` | MongoDB adapter |
| 6 | `reasonr-kafka` | Kafka adapter |
| 7 | `reasonr-vault` | Vault Transit KMS adapter |
| 8 | `reasonr-web` | React: `runtime`, `std-ui`, `vite` (`reasonr-ui`); Go plugins: the embeddable bundle (`registry.RegisterUIBundle` of `port/registry`) and the web test driver (`kind.RegisterDriver`, `reasonrd test --driver web`) |
| 9 | `reasonr-dist` | the standard `reasonrd` (§4.2): its manifest and the generated binary, configuration examples, the end-to-end tests and the tests that need several modules (plan 0022, decision 6) |
| 10 | `reasonr-auth` | authentication module: Auth model and Account app; no Go code (Keycloak tokens go through the engine's `jwt` settings) |
| — | `reasonr-mobile` | React Native — created when roadmap item 15 starts, not in this stage |

Pilot (open source like the framework — §8.2; split by access — §2.1, decision 5):

| # | Repository | Contents | Access |
|---|---|---|---|
| 11 | `pilot-model` | contexts, apps, declarations of own primitives, tests, fakes, demo | everyone |
| 12 | `pilot-integrations` | external system services and adapters | integration engineers, engine developers |
| 13 | `pilot-ui` | implementations of own primitives: `web/` now, `mobile/` later | UI developers, engine developers |
| 14 | `pilot-platform` | engine build with adapters, `engine.yaml`, images, debug environment, deployment, end-to-end tests | engine developers |

The pilot repositories start as the taxi example split by access (§2.2, proposal 9) and work end to end from day one; the pilot itself (roadmap item 13) replaces the model.

## 2. Decisions

### 2.1 Agreed (chat, 2026-11-22)

1. **History.** New repositories start with a clean history. This repository is archived read-only when the stage is done.
2. **UI is not in the engine.** The compiler and the engine keep the abstraction: catalog declarations, UI program, `uiview`, serving the program through the API. Each UI technology is its own repository: `reasonr-web` (React) now, `reasonr-mobile` (React Native) when mobile starts.
3. **Layered modules.** One repository and one version per module: model on ReasonR, Go adapters, UI primitive implementations if any. `reasonr-auth` is one repository; further providers are sibling Go modules in it.
4. **Examples.** Bank and taxi models with their tests stay with the compiler.
5. **Pilot split by access**, not by layer convenience: it shows how a real team works on a model. Model — everyone; integrations — integration and engine developers; controls — UI and engine developers; platform — engine developers.
6. **Engine kinds** other than the default are their own repositories. Stateless stays in the engine core as the default engine (`reasonrd dev`, tests). PostgreSQL is not part of stateless: every engine kind uses the same adapter, so it is its own repository.
7. **Documentation and plans** live in the repository they describe; the parts evolve in parallel.
8. **Owner** — `github.com/tycheg` for now; organizations later.
9. **Pilot UI texts** are English (`texts.en.rr`).
10. **Taxi example** `platform` and `integrations` move to the pilot (`pilot-platform`, `pilot-ui`, `pilot-integrations`).

### 2.2 Proposed

1. **Restructure here, then copy.** All structural changes (§5) are done in this repository on branches, one `make check` over everything validates each step, and cross-references are fixed in one place. The copy into a new repository (§6) is verbatim: a directory tree at a tagged commit, no edits. The alternative — restructuring while copying — validates nothing until every repository builds against published versions of the others.
2. **Module paths** change in this repository before the copy (§5, R8): `github.com/tycheg/reasonr/compiler` → `github.com/tycheg/reasonr-compiler`, and so on (§3). Until the repositories exist, `go.work` replaces resolve them locally.
3. **Plans.** Completed plans with their `*-progress.md` files are carried as a record of the decisions and the progress, placed by subject like the active ones. Active plans are carried with their open part — 0007 (S8), 0011 (B6), 0015 (S3, S4), 0017 (L3, L4), 0018 (W3) — and this plan. Carried plans keep their numbers so references stay valid; new plans in each repository take the next free number. Decisions of completed plans that are still normative are already in the RFC and READMEs; comments cite the RFC or README section, not a plan.
4. **RFC** is split into chapter files (5299 lines; the limit is 1000 per file). Example sync (`docs/`, `go test . -update`) keeps working over chapters.
5. **Versions** start at `v0.1.0` in every repository. Tags, npm publishing, repository creation and archiving are outward actions: only on the author's command.
6. **No duplicate example models.** The engine stops keeping copies of the taxi and bank models (`engine/testdata/{taxi,bank}`): its tests read them from the compiler module at the required version. `engine/testdata/constructs` stays.
7. **Example models require `reasonr-auth` by version** with a committed `reasonr.lock`, like any project; CI runs `reasonr mod download`. Bootstrapping: the auth model tag (`v0.1.0`) is published before the compiler needs it; the Keycloak Go module has its own tag (`keycloak/v0.1.0`) after the engine.
8. **Bank example** `platform` and `integrations` are not carried (the pilot is taxi) — question §8.1.
9. **Pilot seed.** `pilot-model` starts as a copy of the taxi example model, so the four pilot repositories, their versions and access work end to end before the real pilot starts.

## 3. Repositories, modules and dependencies

| Repository | Go modules | npm | Requires |
|---|---|---|---|
| `reasonr-compiler` | `github.com/tycheg/reasonr-compiler`, `…/docs` | `reasonr` VS Code extension (not published) | — (example models: `reasonr-auth` model) |
| `reasonr-engine` | `github.com/tycheg/reasonr-engine` (core), `…/port` (the contract of plugins) | — | core: compiler, port; port: — |
| `reasonr-stateless` | `github.com/tycheg/reasonr-stateless` | — | engine (`kind`), port, compiler |
| `reasonr-postgres` | `github.com/tycheg/reasonr-postgres` | — | port (`port`, `port/registry`; tests: `port/porttest`) |
| `reasonr-mongodb` | `github.com/tycheg/reasonr-mongodb` | — | the same |
| `reasonr-kafka` | `github.com/tycheg/reasonr-kafka` | — | the same |
| `reasonr-vault` | `github.com/tycheg/reasonr-vault` | — | the same |
| `reasonr-web` | `github.com/tycheg/reasonr-web/bundle`, `…/driver` | `@tycheg/reasonr-runtime`, `-std-ui`, `-vite` (§8.3) | bundle: port; driver: engine (`kind`), port, compiler (UI program and test step types) |
| `reasonr-dist` | `github.com/tycheg/reasonr-dist` | — | everything; nothing requires it |
| `reasonr-auth` | — | — | — (a ReasonR module only) |
| `pilot-model` | — | — | `reasonr-auth` model |
| `pilot-integrations` | own services | — | model version (contract) |
| `pilot-ui` | — | own primitives package | web packages, model version (`reasonr build --ui-types`) |
| `pilot-platform` | own adapters (`webhook`) | — | port; its binary is a manifest (engine, stateless, postgres, vault) built by `reasonr-build`; model, ui and integrations versions |

`scripts/deps-check.sh` enforces the table on what each Go module reaches and requires (plan 0022, §3) and the direct imports between the repositories as an allowlist (plan 0021, §5). The adapter, engine kind and web repositories keep `go.mod` at the root, the Go sources in `src/` (the import path ends in `/src`, tags stay `v0.1.0`) and the documentation in `docs/`.

Release order: compiler → port (`port/v0.1.0`) → engine core → stateless, adapters, web → distribution (`reasonr-dist`). The adapters do not wait for the engine: they require only the contract, whose version moves apart from the engine's.

## 4. Source map

### 4.1 Paths

| Now | Repository and path |
|---|---|
| `compiler/` | `reasonr-compiler/` (root module) |
| `compiler/vscode` | `reasonr-compiler/vscode` |
| `docs/` (Go module, RFC, sync test) | `reasonr-compiler/docs` |
| `docs/roadmap.md` | `reasonr-compiler/docs/roadmap.md` (framework); pilot items → `pilot-platform/docs/roadmap.md` |
| `examples/{taxi,bank}/model` | `reasonr-compiler/examples/{taxi,bank}` |
| `examples/taxi/platform/src/engine`, `Dockerfile`, `engine*.yaml`, `compose.dev.yaml` | `pilot-platform/` |
| `examples/taxi/platform/src/ui` | `pilot-ui/web/` |
| `examples/taxi/integrations` | `pilot-integrations/` |
| `examples/taxi/model` (copy) | `pilot-model/` |
| `examples/bank/{platform,integrations}` | not carried (§8.1) |
| `engine/` except below | `reasonr-engine/` (core module; everything but `kind`, `kind/kindtest`, `server` and the builder `cmd/reasonr-build` under `internal/`; `port` is the contract module) |
| `dist/` | `reasonr-dist/` |
| `stateless/` | `reasonr-stateless/` |
| `vault/` | `reasonr-vault/` |
| `engine/cmd/reasonrd/server` | `reasonr-engine/server` (core, §5 R4) |
| `engine/cmd/reasonrd` (main) | `reasonr-dist/cmd/reasonrd`, generated from `reasonr-dist/components.yaml` (plan 0022) |
| `engine/adapters/{postgres,mongodb,kafka}` | `reasonr-{postgres,mongodb,kafka}/` |
| `engine/web`, `engine/adapters/http/webui/dist` | `reasonr-web/` (packages), `reasonr-web/bundle` |
| `engine/testrun/web` | `reasonr-web/driver` (an engine adapter; the `reasonr-web` program is gone, plan 0021) |
| `modules/auth/{reasonr.yaml,src}` | `reasonr-auth/` (root is the ReasonR module) |
| `modules/auth/engine` | removed: Keycloak is `jwt` settings (plan 0021, decision 10) |
| `Makefile`, `.github/workflows/ci.yml` | split: each repository gets its own targets and workflow |
| `AGENTS.md` | `reasonr-compiler/AGENTS.md` (language, `reasonr`), engine parts (`reasonrd test`, HTTP API, MCP) → `reasonr-engine/AGENTS.md`; `pilot-model/AGENTS.md` combines both for the team |
| `README.md` | per repository by section (§4.3) |

### 4.2 The `reasonrd` distribution

`reasonrd` today links every adapter (`engine/cmd/reasonrd/main.go`), and its server imports a second engine kind directly. After the split:

- the server is core and imports no engine kind and no adapter;
- an engine kind registers itself through the registry the way adapters do: the kind (`engine:`) and its subcommands;
- the web bundle registers itself too; without a registered bundle the server serves the API and the UI program only;
- `reasonr-dist` is the distribution: a manifest of the core, the official plugins and the web bundle, and `cmd/reasonrd` generated from it — what `go install github.com/tycheg/reasonr-dist/cmd/reasonrd@v0.1.0` gives (plan 0022);
- a platform (the pilot) builds its own binary from a manifest of the plugins it needs with `reasonr-build` or the `reasonr-builder` image.

### 4.3 Documentation

| Repository | Docs |
|---|---|
| compiler | README (install, language tools, project layout, links to every repository), RFC chapters, AGENTS.md, framework roadmap, plans 0007 (S8), 0011 (B6), 0015 (S3), 0020 |
| engine | README: server, `engine.yaml`, integrations, bus, fakes, personal data, feature flags, HTTP API, MCP; AGENTS.md engine part; plans 0015 (S4), 0017 (L3, L4), 0018 (W3) |
| postgres, mongodb, kafka | README: settings, schema, tests |
| web | README «Interface» section: runtime, catalog, own primitives, `reasonr-ui`, web test driver |
| auth | `modules/auth/README.md` |
| pilot | README per repository (as in the taxi example parts); pilot roadmap in `pilot-platform` |

## 5. Restructuring here (R)

Each step is a branch with `make check`, `make install` and `make images` green and the documented commands run as a user.

| Step | What |
|---|---|
| R1 | Test support packages become public: `engine/internal/enginetest` and `engine/internal/testmodels` → `engine/enginetest`, `engine/testmodels` (the engine kinds, postgres, mongodb, web driver and reasonrd import them across modules) |
| R2 | `docs/` moves into `compiler/docs` (its own Go module); example models move into the compiler module: `examples/{taxi,bank}/model` → `compiler/examples/{taxi,bank}`; RFC sync writes only there; `testmodels` reads the compiler module directory (`go list -m`), `engine/testdata/{taxi,bank}` are removed |
| R3 | Engine kind registration (§4.2): an engine kind and its subcommands through the registry; `server` stops importing engine kinds; each kind becomes its own Go module with its tests |
| R4 | `engine/cmd/reasonrd/server` → `engine/server` (core); `engine/adapters/{postgres,mongodb,kafka}` → top-level `postgres/`, `mongodb/`, `kafka/`; server tests that need postgres, mongodb or kafka move to the adapter modules or to the distribution module; `cmd/reasonrd` keeps only `main` and the distribution checks |
| R5 | Web: `engine/web` → `web/`, `engine/testrun/web` → `web/driver`, the embedded bundle → `web/bundle` (its own Go module, registered by the distribution) |
| R6 | Auth: `modules/auth` → `auth/` with the model at the root and `auth/keycloak` as the Go module |
| R7 | Pilot staging: taxi `platform` → `pilot/platform`, its `ui` → `pilot/ui/web`, `integrations` → `pilot/integrations`, model copy → `pilot/model`; bank `platform` and `integrations` removed (after §8.1); `make images` builds the pilot platform image |
| R8 | Module paths (§2.2, proposal 2) and npm names (§8.3); `go.work` replaces for repositories that do not exist yet; a dependency test fails if a future repository imports one it must not (§3) |

After R8 the top level of this repository is exactly the future repositories: `compiler/` (with `docs/` inside), `engine/`, `postgres/`, `mongodb/`, `kafka/`, `web/`, `auth/`, `pilot/{model,integrations,ui,platform}`.

## 6. Copy and publish (C) — on command

For each repository in release order (§3):

1. Create the empty repository (public — §8.2; collaborators per repository as in §1).
2. Copy its directory at a tagged commit of this repository (`git archive`), add `LICENSE`, `.gitignore`, its `Makefile` and CI workflow; one initial commit: `Initial import from tycheg/reasonr@<sha>`.
3. Replace `go.work` replaces and `file:` npm dependencies with published versions of the repositories already released.
4. CI green with dependencies only from published versions; then tag `v0.1.0` (and submodule tags), publish npm packages.
5. Run every command of its README as a user on a clean checkout.

Then:

6. Old repository: README replaced with a short pointer to the new repositories; archived read-only on GitHub.
7. Memory and local tooling (this assistant's notes, `make` habits) point to the new repositories.

## 7. Done when

1. Each repository builds and passes its CI on its own, requiring the others only by published versions.
2. On a clean machine: `go install …/reasonr-compiler/cmd/reasonr@v0.1.0` and `…/reasonr-engine/cmd/reasonrd@v0.1.0`; `reasonrd dev` on a taxi example serves the API and the UI.
3. The compiler repository: `reasonr check` and `reasonrd test` of both example models pass with the released engine; the engine, engine kind and adapter test suites that use the example models read them from the released compiler module.
4. Pilot, per role on a clean machine with only that role's access:
   - product engineer: clone `pilot-model`, `reasonr test`, `reasonr dev` with the platform image from the registry, own controls visible;
   - integration engineer: `pilot-integrations` tests against the model version, local debug against `reasonrd dev`;
   - UI developer: `pilot-ui` regenerates primitive interfaces from the model version, typechecks, tests, publishes its package;
   - engine developer: `pilot-platform` builds the image from all versions and passes the end-to-end tests.
5. A change crossing roles goes through in order: model under a feature flag → ui and integrations → platform bumps versions and enables the flag.
6. This repository is archived and points to the new ones.

## 8. Answers (2026-11-22)

1. **Bank example** `platform` and `integrations` are not carried (proposal 2.2.8).
2. **Visibility** (revised 2026-11-27 and 2026-11-28): the framework and the pilot repositories (§1) are open source. A private repository of plugins needs `GOPRIVATE` and git credentials for `go` and `reasonr mod download`, and its CI a token with read access to the repositories it requires. Plugins of other repositories, open or private, are linked by a manifest of their own (`reasonr-build`, §4.2); the open-source repositories do not name the private ones.
3. **npm** (revised 2026-11-28): the public registry npmjs.com under the scope `@tycheg` (`@tycheg/reasonr-runtime`, `@tycheg/reasonr-std-ui`, `@tycheg/reasonr-vite`), installed without a token; renamed with the organization. The GitHub Packages registry asks for a token even to install a public package.
4. **Names**: `reasonr-compiler`; `pilot-*` until the pilot has a product name.
5. **Access**: per collaborator and per repository on the personal account.

## 9. Progress

### Restructuring (2026-11-22)

Done in the order R1, R7, R2, R3a, R4, R3b, R4b, R5, R6, R8, each on its own branch (`split-r…`) with `make check`, `make install` and `make images` green. R7 came before R2 because the taxi platform and integrations referred to the example model by relative paths; R3 was split around R4 because the commands of an engine kind need the server in the core.

Deviations from §4–§5:

1. **The Go workspace is at the repository root** (`go.work`): modules now live outside `engine/`. The pilot keeps its own (`pilot/go.work`).
2. **Engine kinds and commands are registries.** `stateless.RegisterKind` (with `Tool` for tool processes and `TestRoles`) and `server.RegisterCommand`; `config.Settings` is the section named after the engine kind. `server.OpenTool` opens an engine for a tool command, `server.LoadPrograms` compiles tenant models.
3. **Tests of the server that need adapters** are end-to-end tests of the distribution (`engine/cmd/reasonrd/e2e`); `engine/server/servertest` starts the server, calls it and runs acceptance tests through it, taking the entry point as an argument so the server's own tests use it too.
4. **The web bundle depends on the engine core**: it registers itself as the default UI (`appapi.SetDefaultBundle`). The web driver tests build their own small reasonrd (core + bundle). The runtime tests use copies of the `engine/uiview` golden files (`web/packages/runtime/test/fixtures`).
5. **The RFC example cache** is keyed by the RFC text, the repository path and a layout version, and writes `src: .` into the examples' `reasonr.yaml`.
6. **`reasonrd build` test** builds with its fake adapter only: the core does not reach adapter modules.

Found on the way, failing on `main` before the split as well; fixed on their own branches ( `fix-kafka-up`, `fix-kafka-tests`, `fix-runtime-tests`, `fix-flaky-timeouts`):

1. `TestKafkaBus` and `TestIntegrationsContract` with `REASONR_TEST_KAFKA` expect topics named by short type names (`event.Order`); the engine names them by full names (`event.Ordering.Order`). CI sets no Kafka, so they were not run.
2. Six tests of `@tycheg/reasonr-runtime` (`packages/runtime/test/runtime.test.tsx`) fail: `make web-check` fails, the build itself matches the committed one.
3. `make kafka-up` never finishes: its readiness probe runs inside the container against the advertised `localhost:19092`.
4. Under the full `make check` load `TestDev` (10 s watcher deadline) and one step of the web driver test (10 s browser timeout) failed once each and passed on rerun.

The fixes: Kafka gets an internal listener for its readiness probe; the Kafka tests expect full names and CI runs them against a Kafka container; the runtime tests open screens by full names; `reasonrd dev` takes the file snapshot before compiling, so a change made while the engine builds is not lost (the real cause of the `TestDev` failures); the web driver test gives a page element 30 s.

### Documentation and plans (2026-11-24)

1. **The RFC** is `docs/rfc/0001-reasonr-language/`: an index (`README.md`) and ten chapter files, each under 1000 lines, with the section numbers unchanged; the block loaders, the example sync and the VS Code grammar test read the chapters in name order.
2. **Each part has its own README**: the [engine](https://github.com/tycheg/reasonr-engine/blob/main/README.md) (with its [AGENTS.md](https://github.com/tycheg/reasonr-engine/blob/main/AGENTS.md)), [postgres](https://github.com/tycheg/reasonr-postgres/blob/main/README.md), [mongodb](https://github.com/tycheg/reasonr-mongodb/blob/main/README.md), [kafka](https://github.com/tycheg/reasonr-kafka/blob/main/README.md), [web](https://github.com/tycheg/reasonr-web/blob/main/README.md); the compiler keeps the language parts of the former root ones ([README](../../README.md), [AGENTS.md](../../AGENTS.md)). The root README and AGENTS.md of this repository are short pointers to the parts.
3. **Carried plans are placed by repository**: 0007, 0011 and 0015 in the compiler; 0015, 0017 and 0018 in [engine/docs/plans](https://github.com/tycheg/reasonr-engine/tree/main/docs/plans). Plan 0015 is in both the compiler and the engine (S3 and S4 are open in different repositories); the two copies differ only in the roadmap link. The plan 0019 progress section gives paths in the new layout.
4. **Completed plans are kept, placed by subject**: 0001, 0002, 0008–0010, 0012, 0014 in the compiler; 0003–0006, 0013, 0016 in the engine (2026-11-26; they were removed on 2026-11-24 and restored on request). Each has a note that its paths and RFC section numbers are those of the time. Comments and the carried plans cite the RFC sections; the roadmap cites both the RFC section and the plan.
5. **Links to another part** are GitHub URLs of the new repositories (they resolve after §6); links inside a part stay relative. The pilot roadmap items (14, 17) moved to [pilot/platform/docs/roadmap.md](https://github.com/tycheg/pilot-platform/blob/main/docs/roadmap.md) and keep their numbers; the link becomes a `pilot-platform` URL in §6.
6. **The engine copy of the constructs test model** is the compiler's (`lower/testdata/constructs`).
7. **Stale cached test results.** The engine, engine kind, adapter and web driver tests read the example models from the compiler module, outside their own module, and `go test` reused cached results after the examples changed. `make test` runs those modules with `-count=1`.

### Copy and publish (2026-11-28)

Copied from `tycheg/reasonr@d105ee51` (branch `phase-c`: npm on npmjs.com, the pilot open source) in the release order, each repository one import commit `Initial import from tycheg/reasonr@<sha>` and the commits after it; every repository was checked as its CI does before its tags, on PostgreSQL, MongoDB, Kafka and Vault where it has tests on them.

| Repository | Tags | CI |
|---|---|---|
| `reasonr-auth` | `v0.1.0` | green |
| `reasonr-compiler` | `v0.1.0`, `docs/v0.1.0` | green (the examples on the released `reasonrd`) |
| `reasonr-engine` | `port/v0.1.0`, `v0.1.0`, `v0.1.1`, `v0.1.2` | green |
| `reasonr-stateless`, `reasonr-postgres`, `reasonr-mongodb`, `reasonr-kafka`, `reasonr-vault` | `v0.1.0` | green |
| `reasonr-web` | `v0.1.0`, `bundle/v0.1.0`, `driver/v0.1.0` | green; the npm packages wait for an npm account with the `@tycheg` scope |
| `reasonr-dist` | `v0.1.0`, `v0.1.1` | `main` waits for the npm packages (the lock of `integration/web/testdata/ui`) |
| `pilot-model`, `pilot-integrations` | `v0.1.0` | green |
| `pilot-ui`, `pilot-platform` | — | wait for the npm packages |

Deviations from §6:

1. **Tags before `main`.** A repository whose CI needs a later one (`reasonr-auth` and the compiler's examples need the released `reasonrd`, the distribution's web job needs the npm packages) got its tags first and its `main` when the later one was released.
2. **A repository of two modules that require one another's release** gets two commits: the import with the tag of the required module, then its `go.sum` with the tag of the other (`reasonr-engine`: `port/v0.1.0`, then `v0.1.0`).
3. **`go.work` in `reasonr-engine`**: the core and `port` are developed together, the core's `go.mod` requires a released contract (a versioned replace: Go reads the `go.mod` of every requirement).
4. **The example models require `reasonr-auth` by version** with `reasonr.lock` (proposal 2.2.7); `make modules` puts the module into the cache in every repository whose tests read them. The engine's `testmodels.Modules` takes the module from the example's `replace` or the cache; the RFC example cache copies `reasonr.lock`.
5. **The `reasonr-build` test** builds with a test engine kind (`testdata/fakekind`, the `kindtest` double) instead of stateless, which requires the engine. The `reasonr-builder` image builds from the engine repository alone; the pilot platform image installs `reasonr-build` with `go install` instead of starting from `reasonr-builder`.
6. **`TestPlatformUI`'s UI build** moved from `reasonr-web/testdata/ui` to `reasonr-dist/integration/web/testdata/ui`, where the test is: the web packages from npm, the taxi model of the compiler module (`REASONR_TAXI`).
7. **The pilot parts read one another by version**: `make deps` checks out `pilot-model` at `MODEL` and `pilot-ui` at `UI` (the Makefile) into `.deps/`; `PILOT_MODEL` and `PILOT_UI` take checkouts instead.
8. **Found by the race detector and fixed**, each failing in the CI of the parts before the split as well (`make check` here ran the tests without `-race`): the coordinator of a main store without one made lazily from two goroutines; the request body and projection rows built in the arena of a committed unit that other consumers read at the same time (`storekit.Own`); the bus woken before the events consumer's cursor moved (`dispatch.Mover`; `TestBus` failed under load). `staticcheck` 2026.2.1 cannot read the export data of Go 1.27.4: CI uses 2026.2.2.

