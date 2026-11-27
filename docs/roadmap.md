# ReasonR roadmap

| | |
|---|---|
| **Purpose** | The top-level plan of the framework: what is done and what comes next. The language is described in [RFC-0001](rfc/0001-reasonr-language/README.md) |
| **How it is kept** | An item is taken into work with its own plan `docs/plans/NNNN-*.md` in the repository it concerns (decisions, stages, progress); here — a link to the plan and the status. New directions are added to §2. The pilot's roadmap is in the pilot platform repository (`pilot-platform/docs/roadmap.md`) |
| **Updated** | 2026-11-27 |

Completed plans are kept as a record of the decisions and the progress; their normative decisions are in the RFC sections named below.

---

## 1. Done

| Stage | Result | Done when | Where |
|---|---|---|---|
| **0. RFC** | The language specification, the grammar, examples | The RFC is accepted | [RFC-0001](rfc/0001-reasonr-language/README.md) |
| **1. Compiler** | The lexer, the parser, semantic analysis, diagnostics, `fmt`, a basic LSP, the module and extensions (`extend`, `on`, `rule`) | The examples compile; negative tests for each error | RFC §28; [plan 0001](plans/0001-compiler.md) |
| **2. Interpreter + in-memory engine** | Bytecode, the VM, the test runner `--driver memory` | The taxi tests pass; 0 allocations per command | RFC §29; [plan 0002](plans/0002-interpreter.md) |
| **3. Stateless engine, PostgreSQL, HTTP API, composable infrastructure** | The engine ports and kernel, the memory and PostgreSQL adapters, storage, uniqueness, timers, the outbox, migrations, the HTTP API, notifications, `engine.yaml`, `--driver api` | The same tests through the API; port conformance tests; a load test | RFC §30–31; [plan 0003](https://github.com/tycheg/reasonr-engine/blob/main/docs/plans/0003-engine.md) |
| **4. Personal data** | Encryption, blind indexes, `forget`, `reasonr pii`, KMS, the `forget` log, migrations and operations | Crypto-shredding tests on memory and through the API; personal data conformance tests; a load test with encryption | RFC §20; [plan 0004](https://github.com/tycheg/reasonr-engine/blob/main/docs/plans/0004-personal-data.md) |
| **5. Integrations** | The external system role, `timeout` as a model timer, `*.fake.rr` fakes, the request delivery queue, the integration service contract and JWT, the CloudEvents bus and Kafka, projections by version | The taxi end-to-end scenario on fakes in memory and through the API; conformance tests of the request queue, the bus and projections; a load test with the bus and the integration service | RFC §18; [plan 0005](https://github.com/tycheg/reasonr-engine/blob/main/docs/plans/0005-integrations.md) |
| **6. UI** | Apps, screens, primitives, the standard catalog on React, the automatic UI, the app API, `--driver web` with places | The same tests through a browser; every taxi place is covered | RFC §22; [plan 0006](https://github.com/tycheg/reasonr-engine/blob/main/docs/plans/0006-ui.md) |
| **Repository structure** | Docs, examples of three parts (model, integrations, platform), the compiler and the engine apart | `make check` in each part; the compiler does not depend on the engine | [plan 0007](plans/0007-structure.md); S8 (tags and publishing) — on command, replaced by phase C of [plan 0020](plans/0020-split.md) |
| **Runs, tests and debugging by role** | `reasonr test`, `reasonr dev`, the `memory,api,web` drivers, `reasonr.yaml`, the platform image, `reasonr-ui` | The commands of the example READMEs work by role | RFC §33; [plan 0008](plans/0008-devtools.md) |
| **Modularity** | Bounded contexts (namespaces, visibility), apps and primitives, extensions by project structure, modules: `requires`, a cache, `reasonr.lock`, `reasonr mod`; the first module is authorization through Keycloak ([reasonr-auth](https://github.com/tycheg/reasonr-auth)) | The taxi is split into bounded contexts, the customer is linked to the `Auth` user; tests on `memory`, `api`, `web`; `reasonr dev` | RFC §23; [plan 0009](plans/0009-modules.md) |
| **Feature flags** | `feature tips "YYYY-MM-DD"`, a flag is a system `bool` in code and layout, nesting is an error, the `feature` line in tests; rollout actor → role in a tenant → tenant → system without a restart (`reasonrd feature`), a log; `reasonr feature list\|remove`; LSP | The taxi: the `tips` flag on `memory`, `api`, `web`; rollout in `reasonrd dev`; after `reasonr feature remove` `check` and `test` pass | RFC §23.4; [plan 0010](plans/0010-feature-flags.md) |
| **Customer customization** | Tenant extensions: `extend`, `on`, `rule`, `extend screen`, a program per tenant, the base tests on each tenant. The isolation modes "a schema per tenant" and "a database per tenant" were not done — dropped 2026-11-12 | The acme tenant passes the base tests (except the excluded ones) and its own | RFC §28, §30–31, §23 |

---

## 2. Next

The order is the priority. The former RFC stages 7–10: 7 is done (customization, §1), 8–10 went into items 15, 13 and 22.

1. **Modularity** — *done* (§1, RFC §23, [plan 0009](plans/0009-modules.md)). Bounded contexts, apps (web, mobile, desktop), reusable modules: business modules in ReasonR, engine adapters in Go, UI modules for different languages and platforms, integration modules.
2. **Feature flags** — *done* (§1, RFC §23.4, [plan 0010](plans/0010-feature-flags.md)). Unfinished code is committed to main, but while the flag is off, users do not see it and it does not block releases.
3. **Bounded contexts, anti-corruption layer and aggregates** — *B1–B5 done, B6 partly*, [plan 0011](plans/0011-contexts-acl-aggregates.md). An entity identifier works only inside its own bounded context; bounded contexts are linked only through an ACL that translates one model into another; aggregates are the consistency boundary, and public commands and queries belong to an aggregate; there are no systems — only bounded contexts. The goal is to move a bounded context into a separate service without rewriting.
4. **External API governance** — *done* (RFC §31.3.1, [plan 0012](plans/0012-api-governance.md)). A stable public API contract while the model changes, with almost no code: the contract in the repository, a check in `reasonr check`, `was` and deprecation instead of adapters.
5. **MCP server** — *done* (RFC §31.3.2, [plan 0013](https://github.com/tycheg/reasonr-engine/blob/main/docs/plans/0013-mcp.md)). An interface for agents in addition to the web (later mobile, desktop) for people and OpenAPI for integrations.
6. **Test impact analysis to shorten test runs** — *done* (RFC §27.4, [plan 0014](plans/0014-impact-analysis.md)). Only the tests the current changes (a commit) affect run; main runs the full set. This also covers checking that a change behind feature flags does not change old tests.
7. **Performance tests at 10 million lines of code** — *partly*, [plan 0015](plans/0015-scale-10m.md): the generator, the measurements and the quadratic spots are done, every budget except the recheck after an edit is met; incremental checking (S3) awaits a decision.
8. **Generating test data for demos** — *done* (RFC §27.5, [plan 0016](https://github.com/tycheg/reasonr-engine/blob/main/docs/plans/0016-demo-data.md); without fakes for demo tenants and a choice in reasonrd dev). This also covers test time: the current time with the scenario offset (`time offset`), in a request header.
9. **Performance tests in ReasonR** — *partly*, [plan 0017](https://github.com/tycheg/reasonr-engine/blob/main/docs/plans/0017-load-tests.md).
10. **Test run workers:** a pull model instead of push — *done*, [plan 0018](https://github.com/tycheg/reasonr-engine/blob/main/docs/plans/0018-test-workers.md).
11. **Splitting the repository** — [plan 0020](plans/0020-split.md); before its phase C — small contracts between the repositories and their structure, [plan 0021](plans/0021-contracts.md); adapters as plugins of a contract module and distributions built from a manifest, [plan 0022](plans/0022-plugins.md).
12. **Authentication module** — *the first one is done*: authorization through Keycloak ([reasonr-auth](https://github.com/tycheg/reasonr-auth), RFC §23).
13. **Pilot** — moved to the pilot roadmap (`pilot-platform/docs/roadmap.md`): rewriting a real application in ReasonR by product engineers and an agent.
14. **Debugging tools.**
    1. The program execution flow: why did this happen?
       1. A command is found from the list of events.
       2. The sequence of command shifts is stored with the log; the log and the shifts are in S3.
       3. The code version is stored in S3.
       4. A command is loaded from S3 or from the current log.
       5. From the events, the command parameters and the code the flow is determined approximately: fast but not exact.
       6. From a snapshot and the journal the flow is determined exactly: slow but exact.
    2. Editing an entity.
    3. Custom queries.
    4. Sending commands and queries.
    5. Monitoring, alerting, observability, metrics of different kinds: infrastructure, business, UI/UX.
    6. Logging: incoming requests (commands are already logged), errors, minimal service status information for diagnostics, integrations.
    7. Running tests in VS Code.
    8. An LSP for agents.
    9. A full debugger?
15. **Mobile apps.** A runtime and a catalog for mobile, `--driver mobile`. The criterion of the former stage 8: the same tests on an emulator.
16. **Adoption** — moved to the pilot roadmap (`pilot-platform/docs/roadmap.md`): training, process knowledge tests, docs and videos from tests, a help bot.
17. **Analytics: a Wide Event Table.**
18. **Localization.**
19. **UI themes.**
20. **A local model to protect sensitive data** sent to external APIs.
21. **A desktop app.**
22. **Compiling to Go:** code generation, a plugin or linking. The criterion of the former stage 10: units of work match the interpreter on every test.
23. **A module distribution ecosystem:** business, UI, integrations, the engine.
24. **Modules.**
    1. Business: authentication.
    2. Web UI: MUI, Ant, [theya-design-system](https://github.com/msstewie-afk/theya-design-system).
    3. Backend: PostgreSQL, MongoDB, ClickHouse, Elasticsearch, OpenSearch, Cassandra, ScyllaDB, Kafka, HTTP, gRPC, WebSocket; ultrafast — ScyllaDB, ClickHouse.
    4. Tools — the tools of item 14.
    5. Mobile.
25. **A bounded context's own unit of work and store:** a bounded context separated from the rest of the system's bounded contexts — only asynchronous calls, answering commands and reactions to events (RFC §23).
26. **Finishing bounded context extraction** ([plan 0011](plans/0011-contexts-acl-aggregates.md), B6 and deviations 7–11).
    1. Separate stores of bounded contexts and delivering ACL reactions through the bus (Kafka) instead of the shared main store and the outbox effect stream (together with item 25).
    2. Running the acceptance tests on split processes: the runner waits for reactions to cross the boundary after each step.
    3. Translation to external systems through `acl` (from the API governance design): today external systems are declared `external` in a bounded context.
    4. A user of another bounded context as an actor by a unique field (`actor Auth.User as Customer by account`), when the mapping is not 1:1.
    5. A deadlock when two processes start at once on an empty PostgreSQL database (creating the system tables).
27. **Automating moving a bounded context from one process to another.** Today a bounded context is moved by hand: `contexts:` in the processes' `engine.yaml` is changed and they are restarted, while `reasonr extract` only shows what changes. A command is needed that moves a bounded context without downtime or losses: it stops accepting its commands in the old process, waits until the started units of work and ACL reactions run, hands the effect stream cursor and the routing over to the new process, moves the bounded context's data with separate stores; with a rollback if the new process does not come up.
28. **Incremental model checking** ([plan 0015](plans/0015-scale-10m.md), S3). A repeated `reasonr check` after editing one file at 10 million lines takes 19.9 s against a 3 s budget; the LSP budgets depend on it too (diagnostics after an edit ≤ 500 ms). A cache of check results by bounded context: the hash of the files and the hash of the bounded context interface (public commands and queries, events, types and identifiers); an edit that does not change the interface rechecks only the bodies of its own bounded context, an interface change also rechecks the bounded contexts, tests and apps that see it; the cross-context analyzers (PII, the call graph) work from stored summaries. It needs a rework of sema: checking the bodies of some files against the declarations of the whole model and results by file that can be cached.
29. **Finishing the engine at scale** ([plan 0015](https://github.com/tycheg/reasonr-engine/blob/main/docs/plans/0015-scale-10m.md), S4).
    1. Starting the engine from a built program without compiling: a `reasonr build -o` artifact with the bytecode, the schema and the UI program, reading it (today bytecode is only written — for the hash), `reasonrd --program file`; the budget is a start ≤ 10 s on a 10-million-line model (today `reasonrd --model` compiles it in about 45 s).
    2. A direct engine measurement at 10 million lines (`engine/internal/scale`) apart from the compiler measurement: today loading the program, memory and the test run for 10m are estimates from 1m.
    3. Memory with many tenants: the interpreter is one per model and shared by tenants, but it is not checked whether the stores hold a copy of the schema per tenant; a measurement on hundreds of tenants and removing the copies.
30. **Finishing the performance tests** ([plan 0017](https://github.com/tycheg/reasonr-engine/blob/main/docs/plans/0017-load-tests.md), L3–L4).
    1. The kinds `stress` (steps until failure, the report says what broke first), `spike` (a jump and a drop), `soak` (the `then` checks in every 10-minute window), `scalability` (capacity on several `--config` or `--replicas`).
    2. In `stats` — `lag.projections`, `lag.timers`, `lag.requests` and `cpu`, `memory`, `dbWait` from Prometheus metrics; `recovery` — how soon latencies returned to normal after the load dropped.
    3. The delivery latency to the integration service and to Kafka — the removed `TestLoad` checked it.
    4. Fake delays in `engine.yaml`: `integrations: { Matching: { type: fake, delay: 50ms..200ms } }`.
    5. Distributed load — generators on workers ([plan 0018](https://github.com/tycheg/reasonr-engine/blob/main/docs/plans/0018-test-workers.md)).
    6. Microbenchmarks `reasonrd test --bench`: the `when` step of each acceptance test in memory, ns/op and allocs/op, a comparison with `--baseline`.
    7. A CI example: smoke on each change, load nightly with a base report.
    8. For the author's decision: multiplying a duration by a fraction, to write `stats.p95 <= baseline.p95 * 1.1` instead of `stats.p95 * 10 <= baseline.p95 * 11`.
31. **Feature flags of the framework:** flags in the compiler, the engine, the engine implementations and the adapters, set at build time or at run time, so that a breaking change in one repository does not stop parallel work in the others; with API diff checks of the public packages. After phase C of [plan 0020](plans/0020-split.md); the contracts they guard are in [plan 0021](plans/0021-contracts.md).
32. **Adapters as Go plugins (`.so`)** — the first item after phase C of [plan 0020](plans/0020-split.md), before item 31. An adapter built with `go build -buildmode=plugin` is loaded at start (`plugins: [./reasonr-oracle.so]` in `engine.yaml`) without rebuilding the binary; calls stay in-process. The binary and every plugin must be built with one Go version and one version of every shared module (Linux and macOS, cgo): the contract module of [plan 0022](plans/0022-plugins.md) is small and versioned for that. Until then a plugin is linked by a manifest (plan 0022). Out-of-process plugins and WebAssembly are not planned.
