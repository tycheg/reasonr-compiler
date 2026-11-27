# Plan: running, testing and debugging by engineer role

| | |
|---|---|
| **Basis** | A discussion of 2026-11-06 after [plan 0007](0007-structure.md): frequent cases by role, short commands, acceptance tests |
| **Status** | Approved 2026-11-06 ("Write a plan… start implementing"): the decisions of §2. Stages D1–D8 are done (§6). Done |
| **Branch** | `devtools` from `structure` |
| **Note** | Paths and RFC section numbers are those of the time (the repository `tycheg/reasonr` before the split) |

---

## 1. Cases by frequency

| Case | Who | Command |
|---|---|---|
| Every acceptance test in memory | everyone, most often the product engineer | `reasonr test` |
| One acceptance test in memory | product | `reasonr test src/order.test.rr:58` or `--run "name"` |
| One test with a driver of choice | everyone | `reasonr test --driver web src/order.test.rr:58` |
| Every test in the pipeline: `given` — API, `when` — k runs over the places of web, mobile and one API run | CI | `reasonr test --driver api,web` |
| Debugging API + UI: real databases, the platform engine, own primitives, fakes | everyone | `reasonr dev` |
| The same with real integration services | integration engineer | `reasonr dev --integrations Matching=http://localhost:8090` |
| A primitive test | UI | `npm test` in `platform/src/ui` (the base engine's command `reasonr-ui test`) |
| Debugging a primitive | UI | `npm run dev` (`reasonr-ui dev`): the primitives page and the app with hot reload |
| Testing and running an integration service | integration engineer | their own: `go test`, `go run` in `integrations/src` |

## 2. Decisions

| # | Decision |
|---|---|
| 1 | **Acceptance tests** instead of "scenario tests" everywhere: RFC §27, AGENTS.md, the README, the CLI help and messages, comments. An acceptance test checks a business requirement through the external interfaces (a black box); its written form is a `given`/`when`/`then` scenario |
| 2 | **The platform builds the engine with `go`**: its own `main.go` (importing the adapters, `server.Main`), `go build`/`go run`, debugging in an IDE. `reasonrd build` stays for building from ready adapters without one's own code; `components.yaml` leaves the examples |
| 3 | **`src/` in every part.** At the root of a part — `README.md` and the run configuration; the code and its build configuration (`go.mod`, `package.json`) — in `src/`. `model/src/*.rr` (the RFC sync writes there), `integrations/src/` (a Go module), `platform/src/engine/` (the Go module of the engine and adapters), `platform/src/ui/` (primitives) |
| 4 | **`reasonr.yaml`** — the configuration of a model project in `model/`: `src` (the sources directory, `src` by default) and `platform` — what runs the model: `image: taxi-engine:0.1.0` or `command: reasonrd` (by default `reasonrd` from the PATH). It is neither the model nor the engine configuration |
| 5 | **`reasonr test` and `reasonr dev` start the project's engine** (the engine executes, `reasonr` only starts it: `exec` or `docker`, no build-time dependency). `reasonrd test`/`reasonrd dev` stay engine commands |
| 6 | **The platform describes the debug environment** and puts it into the image: `/etc/reasonr/dev/compose.yaml` and `/etc/reasonr/dev/engine.yaml` (real databases, test mode — fakes work). `reasonr dev` takes them from the image and starts `docker compose` with the model. Without an image — `reasonrd dev` from the PATH: everything in memory |
| 7 | **Test drivers** — `--driver memory|api|web|mobile` (a comma-separated list) instead of `--engine memory|http|web`. API is one more run among the k: for a `when` step with three web places — web runs by place and one through the API. Without `--url` the engine starts a server in test mode itself. Choosing a test — `file:line` (a line inside the test) or `--run` |
| 8 | **Every tenant by default** in `reasonr check`, `reasonr test`, `reasonrd test`; `--tenant name` (`base` is the base model) narrows; `--all-tenants` is removed |
| 9 | **UI tools are in the base engine**: the command `reasonr-ui` of the package `@reasonr/vite` — `test` (Vitest), `dev` (Vite with a proxy for `/app`, `/api`, `/ui/_primitives` — the primitives page), `build`, `types`; the Vite and Vitest configurations are its own. `platform/src/ui` has `package.json` (dependencies, `reasonr-ui …` scripts) and `primitives/*.tsx` with tests |
| 10 | **Local development of an integration service**: `reasonrd dev --integrations System=url` — the `http` transport to the service for these systems (the JWT signing key is generated per run), the rest are fakes. The service's responses in test mode use `Authorization: Test role=<role>, tenant=<tenant>`; the example's service has a `--test-auth` flag |

## 3. Commands

### 3.1. `reasonrd dev [--config file] [--integrations System=url,…] [--ui dir|url] [--listen address] [module]`

- Test mode (`--test`): fakes, `Authorization: Test …`, signing in to the UI by choosing the caller.
- Without `--config` — everything in memory (like `model/engine.local.yaml` of 0007, which goes away); with `--config` — the platform's configuration (real databases).
- `--integrations` replaces the transport of the named systems with `http`; `--ui` — one's own build (`ui.dir`) or the address of a Vite dev server (the engine proxies `/ui/` to it).
- A change of `*.rr` in the module rebuilds the programs and replaces them in the engine without restarting the process; compile errors go to the log, and the previous program keeps working.

### 3.2. `reasonrd test [--driver list] [--url address] [--ui …] [--tenant name] [--run regexp] [path[:line] …]`

- `memory` by default. `api`, `web`, `mobile` without `--url` — a server in test mode in the same process (`--config` — on the configuration's adapters, otherwise in memory).
- `web` runs `reasonr-web` (Playwright); `mobile` — the message "no driver" until its stage.
- The driver list: `given`, `time passes`, system responses — the API; `when` steps and `then` queries — by runs: the places of each UI driver and one API run. `run` in the report is the run number, `driver` the run's driver.
- `path:line` — the test that holds the line; the path is a module directory or a `*.test.rr` file.

### 3.3. `reasonr test|dev [flags] [project directory]`

Reads `reasonr.yaml` (upward from the directory). `command` — `exec <command> test|dev … <src>`. `image` — `test`: `docker run --rm -v <model>:/model <image> test … /model/<src>`; `dev`: the compose file from the image (`docker run --rm --entrypoint cat <image> /etc/reasonr/dev/compose.yaml`) into a temporary file, the variables `REASONR_MODEL`, `REASONR_IMAGE`, `docker compose up`. The flags are passed to the engine as they are.

### 3.4. `reasonr-ui test|dev|build|types` (the package `@reasonr/vite`)

- The root is the directory `platform/src/ui`, the implementations are in `primitives/`, the model is `--model` or `REASONR_MODEL` (for `types` and the primitives page: the UI program of `reasonr build --dump-ui`).
- `dev`: Vite, a proxy for `/app`, `/api`, WebSocket to `--engine` (by default `http://localhost:8080`), `/ui/_primitives` — one's own and the catalog's display and input primitives on values from the model's types (every enum value; an empty and a filled field; an input is controlled, the value is visible).
- `build` — a build into `dist/` with a manifest; `test` — Vitest in jsdom with Ant Design stubs; `types` — `src/reasonr-ui.ts`.

## 4. Stages

A commit per stage; after each — `make check`, `make web-check`, `make examples-ui`.

| Stage | What |
|---|---|
| D1 | Acceptance tests: the rename |
| D2 | `src/` in the parts of the examples, the platform engine through `go` (`src/engine/cmd/<x>-engine`), the RFC sync into `model/src` |
| D3 | Every tenant by default |
| D4 | The `--driver` drivers, a server in process, `file:line`, the API among the k runs |
| D5 | `reasonrd dev`: test mode, `--config`, `--integrations`, `--ui`, model reload; the examples' integration service — `--test-auth`, its test — through `reasonrd dev` |
| D6 | `reasonr.yaml` in `model/` (instead of `platform.version`): `src`, `platform.image` or `platform.command`. `reasonr test` and `reasonr dev` start the project's engine: by a command or an image (`docker run`, the project volume `/model`; `dev` — `docker compose` from the image's `/etc/reasonr/dev/compose.yaml`). Platforms put `compose.dev.yaml` (PostgreSQL and the `dev` engine) and `engine.dev.yaml` (instead of `engine.local.yaml`) into the image. Compiler commands without a path inside a project take `src` |
| D7 | `reasonr-ui` (the package `@reasonr/vite`, `bin/reasonr-ui.mjs`): `dev` (Vite, a proxy for `/app`, `/api` with WebSocket, `/_test`, `/.well-known` to `--engine`; `/ui/_primitives` — the primitives page), `test` (Vitest, jsdom), `build` (`dist/` with a manifest), `types`, `typecheck`. The runtime entry point and the page are the package's `app/`; the plugin wires the implementations by absolute paths and provides `virtual:reasonr/ui-program`. The runtime exports `ScreenContext`. The examples' platform UI — `package.json`, `tsconfig.json`, `primitives/` |
| D8 | The examples' READMEs — cases by role (§1), the READMEs of the parts, the repository README (a model project, `reasonr test`/`dev`), AGENTS.md, RFC §27.3 (drivers, a server in process, `file:line`), §33 (`reasonr test`/`dev`, `reasonr-ui`, `reasonrd dev`, running by role) |
| D6 | `reasonr.yaml`, `reasonr test`/`dev`; the debug environment in the platforms' images (`compose.yaml`, `engine.dev.yaml` with PostgreSQL) |
| D7 | `reasonr-ui`: `test`, `dev` with the primitives page, `build`, `types`; the examples' `platform/src/ui` on it |
| D8 | Documents: the READMEs of the parts and the examples, the repository README, AGENTS.md, RFC §27, §33 |

## 5. Acceptance criteria

1. The commands of §1 work on taxi and bank; the READMEs of the parts hold only them.
2. `reasonr test --driver api,web` on taxi: every place is covered, the API run passes.
3. `reasonr dev` with the platform image starts PostgreSQL and the engine with fakes; without an image — `reasonrd dev` in memory.
4. `reasonrd dev --integrations` with the example's service: a taxi order is paid without fakes.
5. The primitives page shows the taxi `Tag` in every status and the bank's `AccountNumber`.
6. Every test passes; files are under 1000 lines.

## 6. Progress

| Stage | Result |
|---|---|
| D1 | "Scenario" → "acceptance" in every text except the historical plans 0001–0007; RFC §27 has the definition of an acceptance test |
| D2 | `src/` in the parts. The platform engine is the module `example.com/<x>/engine` (`src/engine`): `cmd/<x>-engine/main.go` imports PostgreSQL and its own bus; `components.yaml` is removed. The RFC sync writes into `model/src` |
| D3 | `reasonr check` and `reasonrd test` by default take the base model and every tenant; `--tenant name` — one model, `--tenant base` — the base one (`compiler.Module.Select`, `compiler.Base`); `--all-tenants` is removed |
| D4 | `reasonrd test --driver memory,api,web,mobile` instead of `--engine`: a test's runs follow the drivers of the list (`testrun.Driver`), failures carry `driver` and `run`. Without `--url` the engine starts the test mode server: `reasonrd` and the platform build — in their own process with their own adapters (`--config`), `reasonr-web` — through the binary `$REASONR_ENGINE` (the calling engine passes it). `reasonrd serve --listen`. Choosing a test — `file:line` and `file`; `mobile` — "no driver" |
| D5 | `reasonrd dev`: test mode; without `--config` — everything in memory, the data tenants are `default` and one per extension; systems without a transport are fakes; `--integrations System=url` — the `http` transport (the JWT key is generated per run); `--ui` — `ingress.ui.dir` or `ingress.ui.proxy` (a new key: `/ui/` is proxied to a dev server); watching `*.rr`. The examples' integration services have a `--test-auth` flag, and their tests run in two modes: `jwt` (`engine.yaml`) and `dev`. `model/engine.local.yaml` is removed |

Deviations:

1. D2: the platform engine's test is `cmd/<x>-engine/main_test.go` (next to `main.go`): it builds it with `go build`, and it no longer needs `reasonrd` in the PATH.
2. D2: the build context of the platform image is the root of the part (`docker build -f src/Dockerfile .`): the `engine*.yaml` configurations lie at the root, the code in `src/`. For integrations the context is `src/`.
3. D2: `examples/go.work` includes `engine/adapters/postgres` and `engine/cmd/reasonrd` (the platform engine requires them). The versions of the ReasonR modules in the examples' `go.mod` are `v0.0.0` until publishing (S8 of plan 0007).
4. D3: `reasonr check --against` without `--tenant` compares the base model: a schema belongs to one model.
5. D4: `driver` on a failure — only for runs not in memory (`api`, `web`): the reports of `--driver memory` did not change.
6. D4, found: two bank tests do not pass through the UI — before this plan they were not run through the UI. "Someone else's account is not available": an `AccountNumber` input without an implementation (the automatic UI) sends a `target` that the server rejects with `invalidArgument(rule: type)`. "After deletion the client's name cannot be read": the role `compliance` has no app, and `Forget` is not available through the UI. The fix is in D7 (the primitives page) and in the bank model (an app for `compliance`), by the author's decision.
7. D5: a change of `*.rr` does not swap the program in the running engine but rebuilds the engine in the same process: the new one is built while the old one works, and then replaces it. The in-memory stores hold the model's schema (`space`), and swapping the program would break them. So data in memory is reset on a model change and stays in real databases; model and configuration errors and incompatibility with the stored data keep the previous engine.
8. D5: the examples' `integrations/engine.yaml` stayed — a mode closer to production (signing in by JWT); the integration tests run in both modes.
9. D6: `reasonr dev` gets the debug environment out of the image with `docker create` and `docker cp`, not `docker run --entrypoint cat`: a distroless image has no `cat`.
10. D6: `engine.dev.yaml` uses the `local` KMS with a permanent debug key (not a secret), not `memory`: with a random master key per build the data in the debug database could not be read after the engine was rebuilt (the tenant keys did not unwrap). The platform test checks that the data survives a rebuild.
11. D6: `reasonr test --driver …,web` with an image: the image has no browser, so the test mode server with the platform's UI build (`dev --ui /ui`) runs in the container, and `reasonr-web` on the host runs the tests against it (`--url`). The `reasonr-web` command must be in the PATH.
12. D6: in image mode relative test paths are passed as they are (the container's working directory mirrors the current one): the report has host paths.
13. D7: the plugin `@reasonr/vite` and the command `reasonr-ui` are JavaScript (the plugin's types are `index.d.ts`): Node 20 does not run TypeScript, and a configuration from a package in `node_modules` cannot import a TS plugin. The command passes the Vite and Vitest configurations built in (`src/config.js`).
14. D7: besides `package.json` and `primitives/`, `platform/src/ui` kept `tsconfig.json` (one line `extends: @reasonr/vite/tsconfig.json`: type checking and the editor) and `.npmrc` (`install-links` for `file:` packages until publishing). The implementation interfaces are `primitives/reasonr-ui.ts`.
15. D7: the primitives page shows leaf primitives — display (`binds field`) and input (`binds input`) without children, parts and search; composite ones (`Table`, `Form`, `EntityPicker`, …) are visible on the app screens.
16. After D8, by the author's decision: the platform image is the local one, otherwise from the registry, otherwise built from the platform's sources (`platform.build` in `reasonr.yaml`, the standard `src/Dockerfile`; in the ReasonR repository — with the build contexts `compiler` and `engine`), otherwise an error with a hint. `make images` builds with the same Dockerfile.
17. After D8: `reasonr-ui` is also a command in the PATH (`make install`: `npm install -g` of the package `@reasonr/vite`). Outside a UI build it finds one (a `package.json` with `@reasonr/vite` above or below the current directory), installs the packages if they are missing, and runs its script `npm run <command>` or its `reasonr-ui`; SIGINT and SIGTERM are passed on.
