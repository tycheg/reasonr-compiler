# Plan: adapters as plugins — a contract module and distributions built from a manifest

| | |
|---|---|
| **Basis** | [Plan 0021](0021-contracts.md) and its [audit](0021-contract-audit.md); the author's requirements of 2026-11-27: the refactoring is about fewer dependencies, and today every module but the compiler depends on others; a new adapter must not mean a new engine; an adapter must run as fast as a linked module |
| **Status** | Approved 2026-11-27; done (P1–P7, [progress](0022-plugins-progress.md)). Before phase C of [plan 0020](0020-split.md): the copy publishes this structure |
| **Branch** | `plan-0022` (from `contracts`) |

---

## 1. Goal

Every Go module requires only what the author allows (§3), in its code and in its tests. An adapter is a plugin: a Go module that knows only a small contract module and is linked into a binary by a manifest — no engine source changes, no slower calls than today. The standard `reasonrd` is one such manifest, in a repository of its own that nothing depends on.

## 2. Decisions

| # | Decision |
|---|---|
| 1 | **A plugin is a Go module linked by a manifest** (like xcaddy and the OpenTelemetry Collector Builder). `components.yaml` names the engine, the kinds and the adapters with their versions; the builder generates `main.go` and `go.mod` and builds the binary. Calls stay in-process and are compiled together: as fast as a module added by hand |
| 2 | **The contract of plugins is a module of its own**, `github.com/tycheg/reasonr-engine/port` (the `engine/port` directory, tags `port/vX.Y.Z`): the ports of adapters and their registration, the settings node. It requires nothing but the standard library: no compiler, no engine. Its version moves apart from the engine's: an engine release does not rebuild plugins while the contract holds |
| 3 | **The contract of engine kinds stays in the engine module** (package `kind`): `Kind`, `Engine`, `Host`, `Kernel`, the executor, the value-level stores, the test driver — they carry the compiler's types. Only the engine kinds and the web driver use it |
| 4 | **Ready requests for transports.** An integration transport gets a request with nothing of the compiler: tenant, position, time, deadline, attempt, system, name, idempotency key, body, CloudEvents attributes, topic. The engine's own HTTP transport keeps the values through an internal interface |
| 5 | **Adapter tests are byte-level.** `port/porttest` (in the contract module) runs conformance suites on the byte-level ports with hand-made layouts and rows: tables and key columns, unique indexes, search, timers and their leases, the outbox and its cursor, the event log, external requests, schema rewrites, the hub link, the bus, the KMS, the transport. No model, no compiler, no engine. The model-level runs (the taxi and bank scenarios, personal data and migrations through the kernel on PostgreSQL and MongoDB) move to the distribution |
| 6 | **The distribution is a leaf repository**, `reasonr-dist` (the `dist/` directory until phase C): the manifest of the official set, the generated `cmd/reasonrd`, the end-to-end tests and every test that needs several modules at once (the web driver with a server, the adapters on the example models). Nothing requires it. `engine/cmd/reasonrd` goes away |
| 7 | **The builder** is `cmd/reasonr-build` of the engine module (it requires the engine module only) and an image `reasonr-builder` with the Go toolchain, so that building a distribution needs no local Go. `reasonrd build` becomes this command |
| 8 | **Go plugins (`.so`)** — the first item after phase C (roadmap item 32). Out-of-process plugins (RPC) and WebAssembly are not done at all |

## 3. Dependencies (target)

The author's table: what each Go module may require among the framework modules, in its code and in its tests. Third-party libraries are those of the module's own job.

| Module | May require | Today |
|---|---|---|
| `reasonr-compiler` | — | — |
| `reasonr-engine/port` (the contract) | — | part of the engine module, imports the compiler |
| `reasonr-engine` | compiler, port | compiler |
| `reasonr-engine/cmd/reasonrd` | — (removed: the distribution moves to `reasonr-dist`) | every module of the framework |
| `example.com/fakeadapter` (the builder's test) | port | engine |
| `reasonr-stateless` | engine, port, compiler | engine |
| `reasonr-postgres`, `-mongodb`, `-kafka`, `-vault` | port | engine, **compiler** (through `port`, even in production code, and through `porttest` in tests) |
| `reasonr-web/bundle` | port | engine |
| `reasonr-web/driver` | engine (`kind`), port, compiler | engine, compiler, stateless, bundle (its test server) |
| `reasonr-dist` | everything | — |
| pilot `platform` | port (its own webhook adapter); its binary is a manifest | engine, compiler, postgres, stateless, vault |

`scripts/deps-check.sh` checks this table on `go.mod` (`go list -m all` of the module's packages and tests), not only the direct imports.

## 4. Repository structure (target)

```
reasonr-engine/
  port/                  module …/reasonr-engine/port: no dependencies
    port.go …            the adapter ports: row stores, timers, the outbox, events, requests,
                         projections, coordination, the bus (ready messages), the hub link,
                         integrations (ready requests), KMS, codecs, metrics, the UI bundle
    node.go              the settings node and its YAML parser (from internal/config)
    registry/            Register* of adapters, codecs, commands, the UI bundle
    porttest/            byte-level conformance suites
  kind/                  the kind contract (compiler types): Kind, Engine, Host, Kernel, Executor,
                         the value-level stores, TestDriver; RegisterKind, RegisterDriver, OpenTool
  kind/kindtest/         engines from engine.yaml, a host, the test kind, the model-level suites
  server/                Main, Run
  cmd/reasonr-build/     the builder
  internal/              the rest

reasonr-dist/            components.yaml, cmd/reasonrd (generated), e2e/, integration tests
reasonr-stateless/, reasonr-postgres/, …   as today (src/, docs/)
```

The engine reads what plugins registered through one exported function of `port/registry` (`registry.Registered`), documented as the engine's: Go has no friend packages, and a separate module cannot hide a read side from other importers.

## 5. The manifest

```yaml
# components.yaml
module: github.com/tycheg/reasonr-dist      # the module of the generated binary
binary: reasonrd
engine: github.com/tycheg/reasonr-engine v0.1.0
port: github.com/tycheg/reasonr-engine/port v0.1.0
plugins:                                    # packages that register in init()
  - github.com/tycheg/reasonr-stateless/src v0.1.0
  - github.com/tycheg/reasonr-postgres/src v0.1.0
  - example.com/acme/reasonr-oracle v1.2.0
replace:                                    # local directories (development)
  example.com/acme/reasonr-oracle: ./oracle
```

`reasonr-build -f components.yaml -o reasonrd` builds the binary; `--source dir` writes `main.go` and `go.mod` instead (the distribution and the pilot platform commit them, and CI checks they match the manifest). The builder rejects two versions of the contract module and prints which plugin requires which. `docker run reasonr-builder -f components.yaml` does the same without local Go.

## 6. Stages

| Stage | Content | Done when |
|---|---|---|
| P1 | **The contract module.** `engine/port` becomes a module without dependencies: the adapter ports stay, the settings node and its parser move in, the integration request becomes ready (decision 4), registration of adapters moves to `port/registry`; the kind contract moves to `engine/kind` (with `RegisterKind`, `RegisterDriver`, `OpenTool`) | `go list -deps` of `port/...` has no `reasonr-*` package; every module builds |
| P2 | **Byte-level conformance.** `port/porttest`: the suites of decision 5; today's model-level `porttest` becomes `engine/kind/kindtest` | postgres, mongodb, kafka, vault pass the byte-level suites; their tests import only `port`, `port/registry`, `port/porttest` |
| P3 | **Adapters on the contract only.** Adapters, the web bundle and the builder's test adapter require only `port`; their model-level tests wait for the distribution (P5) | the `go.mod` of each adapter has no `reasonr-engine` and no `reasonr-compiler` |
| P4 | **The builder.** `engine/cmd/reasonr-build` with the manifest of §5 (versions, `plugins`, `replace`, `--source`, one contract version); `reasonrd build` is gone; the `reasonr-builder` image | a binary from a manifest with a third-party adapter of `port` only runs `migrate`; the image builds the same binary |
| P5 | **The distribution.** `dist/` (module `github.com/tycheg/reasonr-dist`): the official manifest, the generated `cmd/reasonrd`, the end-to-end tests from `engine/cmd/reasonrd/e2e`, the adapters' model-level tests, the web driver's browser tests; `engine/cmd/reasonrd` is removed; `make build`, `make install`, `make images` and CI use the distribution | `go.mod` of the web driver — no stateless, bundle, server; of the engine module — only compiler and port |
| P6 | **The pilot platform from a manifest.** `taxi-engine` is a manifest (stateless, postgres, vault and its own webhook adapter, which imports only `port`), built by the builder in its Dockerfile | `make images` builds it; the pilot tests pass |
| P7 | **Enforcement and documentation.** `deps-check.sh` checks §3 on `go.mod` and the imports; READMEs ("Contracts", "Layout", how to write and link a plugin); plan 0020 §1, §3, §4 (the repository `reasonr-dist`, the tag of the contract module, the release order: compiler → port → engine → kinds, adapters, web → dist) | `make check` fails on a requirement outside §3 |

Each stage is its own commit; `make check` with PostgreSQL, MongoDB, Kafka, Vault and the browser after each, and before the last report `make install`, `make images` and the documented commands.

## 7. Not in this plan

- **Go plugins (`.so`)** loaded at start (`plugins:` in engine.yaml) — roadmap item 32, the first after phase C. They need the binary and every plugin built with one Go version and one version of every shared module; the small contract module of this plan is what makes that practical.
- **Out-of-process plugins and WebAssembly** — not done (the author's decision of 2026-11-27).

## 8. Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Byte-level suites miss what the model-level runs catch | Medium | The model-level runs stay, in the distribution, on every change of an adapter (CI of the distribution) |
| The contract module grows back the compiler's types | Medium | `deps-check.sh` on `port/...`; a port that needs a model value belongs to `kind` |
| Building needs the Go toolchain | Certain | The `reasonr-builder` image; the official `reasonrd` is published built |
| An adapter release and the distribution drift | Medium | The distribution pins versions in its manifest; its CI runs the end-to-end tests on each bump |
