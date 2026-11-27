## 31. The default engine implementation

This is **one possible** engine. The model knows nothing about it.

The engine is assembled from components: each kind of infrastructure is a port (a Go interface), a port has adapters, and the configuration picks an adapter and its settings. The default adapters:

| Port | What it stores or carries | Default | Other adapters |
|---|---|---|---|
| Main store | Entities, synchronous projections, timers, the outbox, uniqueness, idempotency keys | PostgreSQL | memory; MongoDB |
| Projection store | Asynchronous projections: filled from the outbox | the main store | OpenSearch, a separate PostgreSQL |
| Event store | Events (audit, analytics); optional | PostgreSQL, a separate database | ClickHouse |
| Bus | All events — from the event store, at least once, ordered within an entity; notifications — by setting, without guarantees; the format is CloudEvents 1.0 | — | Kafka; NATS, RabbitMQ |
| Integration service | External requests are written at commit and sent in parallel with retries; the contract is OpenAPI from the model, JWT both ways (§18) | HTTP | gRPC |
| Notification hub | Notifications between server instances | PostgreSQL LISTEN/NOTIFY | Redis, NATS |
| Incoming calls | The model API | HTTP + JSON | gRPC, TCP, UDP |
| Notification delivery | Notifications to other actors | WebSocket | long polling |
| Telemetry | Logs, metrics, tracing | `slog` (stdout), Prometheus | OpenTelemetry |
| Personal data | A master key in a KMS → a tenant key → a subject key with a version; values — AES-256-GCM, search and uniqueness — HMAC-SHA-256 blind indexes | KMS `local` (a key from the environment), `vault` (Vault Transit) | `memory` (`--test` only), an own KMS adapter |
| Multitenancy | A `tenant` column + RLS | RLS | a schema or database per tenant |
| UI | The app runtime and primitive implementations (§22); the standard catalog `std/ui` | TypeScript + React (web) | React Native, Swift, Kotlin (mobile) |

Only the main store transaction is atomic. Everything else gets the unit of work from the outbox after commit: there are no distributed transactions. So a projection with a unique index, or one whose rows commands check (`if [martId]`), is stored only synchronously — in the main store.

### 31.1. Engine configuration (not part of the model)

```yaml
# engine.yaml
engine: stateless

stores:
  main:   { type: postgres, dsn: "postgres://app@db/main", tenancy: rls }   # rls | schema | database
  search: { type: opensearch, urls: [https://os:9200] }

state: main                       # entities, timers, outbox, external requests to send, uniqueness, keys
projections:
  default: main                   # synchronously, in the commit transaction
  OrderMart: search               # asynchronously from the outbox
events: analytics                 # the event store; without the key events are not stored

crypto:                           # personal data (§20)
  kms: { type: vault, address: "https://vault:8200", key: reasonr, token_env: VAULT_TOKEN }
  # kms: { type: local, key_env: REASONR_MASTER_KEY }   # 32 bytes, base64
  cache: { subjects: 100000, ttl: 5m }                  # subject key cache
  forget_log: audit               # the forget journal outside the main database (§20.3, §33)
features: main                    # feature flag state and journal (§23.4); state by default
contexts: [Dispatch]              # bounded contexts of this process (§23.2): all by default
bus:                              # all events from the event store (events is required)
  type: kafka
  brokers: [kafka:9092]
  topic: "reasonr.{kind}.{type}"  # {tenant}, {kind}: event | notification, {type}
  notifications: true             # notifications — without delivery guarantees
  create_topics: { partitions: 12, replication: 3 }   # reasonrd migrate creates missing topics; without the key — only a check at startup
integrations:                     # requests are written at commit and sent in parallel, each independently
  workers: 64                     # concurrent sends
  retry: { initial: 1s, multiplier: 2, max_interval: 1m, max_elapsed: 1h }   # max_elapsed — for requests without a timer
  auth: { key_env: REASONR_INTEGRATIONS_KEY, kid: "2026-10" }   # the engine's JWT signature (ES256, PEM), §18
  default:  { type: http, url: http://integrations.internal }
  Matching: { type: grpc, target: matching:9090 }
  # Payments: { type: fake }      # a *.fake.rr fake — reasonrd --test only (C0010)
notifications:
  hub: { type: postgres }         # LISTEN/NOTIFY
  delivery: [websocket]           # longpoll is the alternative
ingress:
  - type: http
    listen: ":8080"
    codec: json
    ui:                           # §22 apps on this ingress: the API /app/… and the runtime /ui/… (§31.4)
      dir: ./src/ui/dist          # an own runtime build; without dir — the built-in one (the standard catalog)
      auth: { oidc: { issuer: https://idp, client_id: taxi-web, scopes: [openid, profile] } }
auth:
  type: jwt
  jwks: https://idp/.well-known/jwks.json
  tenant: { claim: tid, any: "*" } # an external system token for every tenant: the tenant from X-Reasonr-Tenant
tenants:                          # data tenant → extension (without extension — the base model, §21.1)
  default:      {}
  acme-prod:    { extension: acme }
  acme-staging: { extension: acme }
telemetry:
  logs:    { format: json, level: info }
  metrics: { type: prometheus, listen: ":9100" }
limits: { retries: 16, consistency_wait: 2s, idempotency_ttl: 24h }
```

`reasonrd check --config engine.yaml <module>` checks the configuration against the model before the server starts: projections that can only be synchronous, store capabilities, external system transports, models with extensions.

### 31.2. The main database (generated)

A row per entity: the state is `jsonb` in canonical JSON (§31.3), typed columns only for index keys, which the engine writes at commit. A new field, a rename (`was`), widening `int` → `bigint` and extension fields need no DDL.

```sql
create table e_order (
  tenant       text   not null,
  id           uuid   not null,          -- the column type follows the id strategy: uuid, bigint, text, jsonb
  version      bigint not null,
  state        jsonb  not null,          -- all fields, extension fields included
  k_customer   uuid,                     -- keys of index byCustomer
  k_placed_at  numeric,                  -- Unix nanoseconds: order and precision as in the model
  primary key (tenant, id)
);
create index i_e_order__by_customer on e_order (tenant, k_customer, k_placed_at desc nulls last, id);

create table e_customer (
  tenant     text   not null,
  id         uuid   not null,
  version    bigint not null,
  state      jsonb  not null,            -- personal fields are null
  sealed     jsonb,                      -- their ciphertexts: field, subjects, data
  subjects   text[],                     -- the row's subjects "Type:id" (GIN index): forget finds rows by them
  k_phone    bytea,                      -- a blind index for search and uniqueness
  primary key (tenant, id)
);
create unique index e_customer_by_phone on e_customer (tenant, k_phone);   -- unique phoneTaken

create table p_order_mart (  -- a projection: as an entity, without a version
  tenant text not null, id uuid not null, state jsonb not null, k_status text, primary key (tenant, id)
);

create table rt_tenant_keys (
  tenant  text  primary key,
  wrapped bytea not null                 -- the tenant key wrapped with the KMS master key
);

create table rt_subject_keys (
  tenant       text   not null,
  subject_type text   not null,
  subject      text   not null,          -- the id as canonical JSON
  version      bigint not null,          -- grows after each forget
  wrapped      bytea,                    -- wrapped with the tenant key; null — destroyed by forget
  primary key (tenant, subject_type, subject)
);

create table rt_timers (
  tenant      text not null,
  id          uuid not null,
  fire_at     bigint not null,           -- Unix nanoseconds
  seq         bigserial,                 -- scheduling order
  lease_until bigint,                    -- the lease of the running instance
  attempts    int  not null default 0,
  dead        text,                      -- the reason; null — alive
  entity_type text not null,
  entity_id   jsonb not null,
  command     text not null,
  args        jsonb not null,
  primary key (tenant, id)
);
create index on rt_timers (fire_at, seq) where dead is null;  -- selecting fired timers: for update skip locked

create table rt_outbox (
  position bigserial primary key,        -- the unit of work position
  tenant   text  not null,
  txid     xid8  not null,               -- the reader does not skip uncommitted transactions
  payload  jsonb not null,               -- events, projection operations, external requests, forget
  trace    text
);

create table rt_idempotency (
  tenant text not null, key text not null, result jsonb not null, expires_at timestamptz not null,
  primary key (tenant, key)
);

create table rt_schema_versions (
  tenant_model text   not null default '',
  version      bigint not null,
  source_hash  text   not null,
  schema       jsonb  not null,
  ir           bytea  not null,
  applied_at   timestamptz not null,
  primary key (tenant_model, version)
);
```

One transaction per unit of work: entity state with version checks, synchronous projections, timers, deleting the fired timer, forget (destroying keys and erasing the subjects' rows), the outbox and the response by idempotency key. Personal values in rows, timers, the outbox and responses by key are stored as ciphertexts. The event store and asynchronous projections in other stores are encrypted with the main store's keys, so forget makes them unreadable too. A unique index violation becomes the business error from the model. Entities that do not exist are not stored.

### 31.3. HTTP API (generated)

| Method | Path | What |
|---|---|---|
| `POST` | `/api/{Entity}/{id}/{Command}` | A public command; the `Idempotency-Key` header is the retry key |
| `GET` | `/api/{Type}/{Query}?...&after=…` | A public query |
| `GET` | `/api/ws` | WebSocket: the actor's notifications from other actors |
| `GET` | `/api/poll?cursor=…&wait=30s` | The same through long polling, if enabled in the configuration |
| `GET` | `/openapi.json` | The OpenAPI of the model with the caller's extension |

The tenant is determined by the token. External systems answer through the same endpoints with a token of their role. OpenAPI is generated from the model, separately for each tenant.

```http
POST /api/Order/0192b6c1-.../Cancel
Authorization: Bearer <customer-jwt>
Idempotency-Key: 7f1c…

{ "reason": "changed my mind" }

→ 422 Unprocessable Entity
{ "status": "rejected", "errors": [ { "name": "cannotCancel", "args": { "currentStatus": "paid" } } ] }
```

| Outcome | Code | Body |
|---|---|---|
| The command is accepted | 200 | `{"status": "accepted", "position": "1043", "notifications": [ … ]}` |
| Business errors | 422 | `{"status": "rejected", "errors": [ … ]}` |
| `forbidden` | 403 | the same, error `forbidden` |
| `invalidArgument` | 400 | the same |
| No authentication | 401 | — |
| No type, method or route | 404 | — |
| An asynchronous projection has not caught up with `after` | 503 + `Retry-After` | error `notReady` |
| An execution fault | 500 | `{"status": "failed", "error": "internalError", "incident": "<id>"}` |

**Notifications.** Notifications to the command initiator come in the body of its response. Notifications to other actors come over WebSocket (`/api/ws`) or long polling (`/api/poll`) as `{"name": …, "args": {…}}` (§13).

**`position` and `after`.** A command response carries the position of its unit of work. A query with `after=<position>` answers once the asynchronous projections it reads have applied that position; the engine waits up to `consistency_wait` (2 s by default), then answers 503 `notReady`. Without `after`, a query reads what is there now. For entities and synchronous projections `after` changes nothing.

**Command retries.** The response to a command with an `Idempotency-Key` is kept for a day (`idempotency_ttl`); a retry with the same key gets the same response, and the command does not run again.

**Canonical JSON of values** — one format in the API, in `state jsonb`, the outbox, exports and logs:

| Type | JSON | Example |
|---|---|---|
| `int`, `float` | a number | `42`, `55.75` |
| `bigint`, a `sequence` identifier | a string with an integer (JavaScript precision is 2⁵³); a number is accepted on input too | `"9007199254740993"` |
| `decimal` | a string | `"190.50"` |
| `string`, `bool` | a string, `true` / `false` | |
| `bytes` | a base64 string | |
| `uuid`, a uuid identifier, `Timer.Id` | a string 8-4-4-4-12 | |
| `date`, `time`, `timestamp` | a string: a date, a time of day, RFC 3339 UTC | `"2026-10-04"`, `"15:04:05"` |
| `duration` | a language literal: the largest unit in which the duration is whole (as `string(x)`, §8); a composite form is accepted on input | `"90s"`, `"90m"`; on input `"1h30m"` |
| enumeration | the value name | `"searching"` |
| a value object, a composite identifier | an object with the fields | |
| `T?` = none | `null`; a missing argument — none or the default | |
| `list`, `set` | an array | |
| `map` | an object if the keys are strings, enumerations or string identifiers; otherwise an array of `[key, value]` pairs | |
| `page<T>` | `{"items": [...], "next": "cursor" \| null}` | |
| `PageRequest` | `{"size": 20, "cursor": null}`; in a query — parameters `size`, `cursor` | |

### 31.3.1. The external API contract

A ReasonR system has an external API — integrators and other teams use it, while the model changes every day. The contract is kept next to the sources, not in the model: an API change review is a diff of its files.

| File | What |
|---|---|
| `api/contract.json` | Published methods (`Ordering.Order.Cancel` or `Ordering.Order.*`) and deprecated ones with a removal date (`sunset`) |
| `api/versions/<YYYY-MM-DD>.json` | A snapshot of a released version: methods, parameters, results, and the value objects and enumerations they reach |

- **Nothing is published by default.** Outside test mode, `/api` exposes only the contract's methods and the answering commands of external systems (the integration service contract); everything else is 404. Test mode exposes every public method. `/openapi.json` shows what is published. The app API (`/app/…`) is not part of the contract.
- **Versions are dates.** `reasonr api release` freezes the current contract. A client names the version with the header `Reasonr-Version: 2026-11-12`; without the header — the current one. There is one implementation — the current model: a client of a version sees the version's names from the snapshot (translations without code).
- **`was` is the former name** of a method, parameter, value object field or enumeration value: `command(customer) Cancel(cancelReason string was reason)`, `query(…) Card(…) OrderCard was Info`, `rub was rubles`. A client of a version calls and receives the former names. A new parameter with a default does not disturb an old version.
- **The check is in `reasonr check`.** The model is compared with each released version: a removed method, parameter, field or value (E1104, with a `was` hint), a new required parameter (E1105), a changed type (E1106), publishing a method that does not exist (E1107). A deprecated method may be removed after its `sunset` date.
- **Deprecation.** `reasonr api deprecate Ordering.Order.Card --sunset 2027-03-01`: `deprecated` in OpenAPI, `Deprecation` and `Sunset` headers in responses. `reasonr api retire 2026-11-12` ends support of a version.
- **Usage.** The metric `reasonr_api_calls_total{method, version}`; `reasonrd api usage --url …` — who is on which version.

```sh
reasonr api publish 'Ordering.Order.*'      # the contract: the order's public methods
reasonr api release                         # a version for today
reasonr api openapi --version 2026-11-12    # OpenAPI as the version's clients see it
```

### 31.3.2. MCP — the interface for agents

A system has three interfaces: the web for people, OpenAPI for integrations, **MCP** (Model Context Protocol) for AI agents working on behalf of a user or a role. The engine builds the MCP server from the model; it needs no code.

- **The entry** is `POST /mcp` on the HTTP ingress (MCP Streamable HTTP, JSON responses): `ingress: [{ type: http, mcp: true }]`, always in test mode. Locally — `reasonrd mcp --stdio --as actor=Customer:<id> [module]`: the engine as in `reasonrd dev`, the agent's messages line by line on stdin and stdout.
- **Authentication is the same as for the API:** a token in `Authorization`, the tenant and the caller from the token; protected resource metadata — `/.well-known/oauth-protected-resource` with the issuers from `auth`.
- **Tools** are the public commands and queries whose access the caller can pass: `Ordering.Customer.OrderTaxi`, `Ordering.Order.Card`. The description is the method's `##` comment, the input is a JSON Schema of the arguments (for a command, also the target `id`), the output is the query result (`result`) or the command response (`status`, `notifications`, `errors`). Queries are marked `readOnlyHint`, commands with `forget` or `delete` — `destructiveHint`: the agent asks the user.
- **A call** goes through the same handlers as `/api`: model errors — `isError: true` with the errors; an agent's repeated call in the same session (`Mcp-Session-Id`) does not run the command twice. The external API contract (§31.3.1) does not restrict MCP: it is a user interface, like the UI. There are no MCP resources or templates: all reading is query tools.
- **Tests** — `reasonrd test --driver mcp`: steps are `tools/call` from the step's caller.

### 31.4. UI

From each app, the compiler builds a platform-independent description of the screens. It is run by the **platform runtime**: binding (§22.4), data loading, forms, transitions, notifications, labels (§22.10); primitive implementations draw the screen. Engine developers decide how the runtime works on each platform:

| Platform | Options |
|---|---|
| web | TypeScript + React |
| mobile | React Native — the runtime is shared with the web; native Swift and Kotlin — the runtime reads the same screen description; a shared runtime core (binding, checks, an interpreter for offline) may be Go through `gomobile` |

**The app API.** The model is not visible to the client: only the operations of the app's screens go out.

| Request | What it does |
|---|---|
| `GET /app/{App}` | The manifest: title, language, menu, screens with description hashes, places |
| `GET /app/{App}/screens/{Screen}.{hash}.json` | A screen description: nodes, links, labels, only the types used; cached forever |
| `GET /app/{App}/{Screen}?parameters` | The screen data in one response: node query results, row visibility, hidden nodes, the read position |
| `GET /app/{App}/{Screen}/{node}?args=…` | A place query: filters, the next page, an `EntityPicker` search |
| `POST /app/{App}/{Screen}/{node}` | A place command: the body is the target and the inputs; the response is as in the model API (§31.3) |
| `GET /app/{App}/openapi.json` | The app contract — for its own clients |
| `GET /ui/…` | The web runtime: the built-in build or `ingress[].ui.dir` |

The schema does not grow with the model: the client loads the manifest and the descriptions of open screens only.

- **Screen data in one request.** The server runs the node queries over the tree in one read snapshot and evaluates `if` (the bytecode of layout expressions). Hidden nodes are not returned and their nested queries do not run; for nodes in list rows, the response gives per-row visibility. The runtime does not evaluate `if` itself: conditions may read fields that are not in the response.
- **Only the app's places.** The app API calls only queries and commands that have a place in its screens, on behalf of the caller and with their access (§19). The model API (`/api/…`) stays for external systems and tests; the engine may close it to browsers.
- **Notifications** — the same WebSocket `/api/ws`; the credentials go in the subprotocol (`reasonr`, `bearer.<token>`), since a browser sends no headers when opening a WebSocket.
- **Sign-in.** In test mode (`reasonrd --test`) the runtime shows a "sign in as" form: actor, identifier, roles, tenant. Otherwise — OIDC (`ingress[].ui.auth.oidc`): authorization code with PKCE; `me` is the token's `sub`.

**Texts** are layout strings and `texts` blocks (§22.11); the screen description and the manifest hold the texts in the user's language (`Accept-Language`, otherwise the app's base language).

**The web runtime and the standard catalog.** The built-in build (`@tycheg/reasonr-runtime` and `@tycheg/reasonr-std-ui`) is React and Ant Design; `reasonrd` serves it without Node.js in production. Primitive implementations live in `primitives/<Primitive>.tsx` of the platform's UI build and are wired in by the `@tycheg/reasonr-vite` plugin; the `reasonr-ui` command of the same package builds (`build`), tests (`test`), checks against the interfaces (`types`, `typecheck`) and shows them (`dev`: apps with hot reload and the primitives page `/ui/_primitives` on values of the model types). `ingress[].ui.dir` points to the build output. An implementation is looked up by name: the module's own → the standard catalog → the automatic UI. Implementations can be replaced one primitive at a time — this is how the component library changes while the layout stays the same. `reasonr build --ui-types` gives the prop types for implementations; at startup `reasonrd` checks the apps' primitives against the build manifest (`reasonr-ui.json`).

---

## 32. Schema and migrations

1. **The business logic schema is stored in the database** (`rt_schema_versions`) for the base model and for each extension.
2. **Data migration is generated by the engine** from the difference between model versions:

| Model change | Action |
|---|---|
| A new entity, projection, query, command, event, rule, app, screen, primitive | Automatic |
| A new entity or projection field | Automatic, with the default value |
| A rename: `fullName string? was name` | Automatic |
| A new index, unique ones included | Automatic; for a unique one the engine first checks the data and stops the migration with a list of violations if there are duplicates |
| A new event argument with a default: `event placed(..., promo string = "")` | Automatic; old events read with that value |
| A field or event parameter became `personal` | Automatic: the engine encrypts the values in rows and builds blind indexes. Events already written to the outbox and the event store stay plaintext — a limitation: the subject of a parameter was computed from the entity state at the time of the event and cannot be restored. `forget` does not erase such values; if they must be hidden, the journal is cleaned with database tools |
| A field stopped being `personal` | Automatic: the engine decrypts the values (`none` for forgotten subjects) and replaces the blind index with an ordinary one |
| Removing an event argument or changing its type | Forbidden; a new event is needed |
| Widening `int` → `bigint` (a field, an event argument, collection elements) | Automatic: the integer encoding is the same, the range is wider |
| Changing a field type | A manual migration |
| Removing a command that scheduled timers refer to | A manual migration |
| A new projection for existing data | A manual fill migration |

3. **Manual migrations** are written in the language. `all <Entity>` is allowed only in migrations. Each command call from a migration is a separate unit of work: in the example below, each iteration.

```rr
migration fillOrderMart
  for orderId in all Order
    [orderId].writeMart()                # the board row from the order fields (taxi/contexts/Ordering/order.rr)
```

4. **Moving data between engines** is an export and import of a canonical format: NDJSON with snapshots, timers, the schema version, encrypted subject keys and events, if the engine stores them. Projections are recomputed by migrations.

---

## 33. Tools

| Tool | Purpose |
|---|---|
| `reasonr check [--extension t]` | Compilation and all checks (human output or `--format json`); by default the base model and every extension, `--extension base` — the base model only |
| `reasonr test`, `reasonr dev` | Running the model on the project's engine (`reasonr.yaml`: `src`, `platform.image` — local or from a registry, `platform.build` — the platform sources to build the image if it is missing, or `platform.command`): acceptance tests (`reasonrd test`) and local debugging (`reasonrd dev`; with an image — the platform's debug environment `/etc/reasonr/dev/compose.yaml`: databases and the engine) |
| `reasonr mod download`, `reasonr mod verify` | Project modules (§23.3): downloading into the module cache and `reasonr.lock`, checking the cache by checksums |
| `reasonr fmt` | The canonical format |
| `reasonrd test [--driver memory,api,web,mobile] [--url address \| --config engine.yaml] [--extension t] [--affected [--base ref] \| --impact report.json] [--seed n] [--coverage] [--place place] [--navigate path] [--artifacts dir] [path[:line]]` | Acceptance tests; on UI drivers — through every place (§27.3); `--artifacts` — screenshots of failures. `--driver web` is in a binary that links the web driver, as `reasonrd` does (the browser — `reasonrd web install`) |
| `reasonrd dev [--config engine.yaml] [--integrations System=url,…] [--ui dir\|url]` | Local debugging: the engine in test mode (fakes, sign-in by choosing the caller), without `--config` — everything in memory; the named systems run on the real integration service; a change of `*.rr` rebuilds the engine without restarting the process |
| `reasonr ui eject <app>` | The layout `auto` builds — a source for your own (§22.9) |
| `reasonr run` | A local run of the API and UI on the in-memory engine |
| `reasonr build [--dump-schema] [--dump-ui] [--dump-impact] [--ui-types]` | Building the model; the schema for compatibility checks; the interface program; the test impact analysis graph (§27.4); TypeScript types for primitive implementations (§31.4) |
| `reasonr impact [--base ref] [--format json] [--allow-test-changes test]` | The tests of each model a change affects, and why; an error — a flag added while an old test changed (§27.4) |
| `reasonrd openapi --integrations` | The OpenAPI of the integration service contract (§18) |
| `reasonr-ui dev\|test\|build\|types\|typecheck` | The platform UI build (package `@tycheg/reasonr-vite`): primitive implementations, their tests, a dev server with a proxy to the engine and the primitives page `/ui/_primitives`, a build with a manifest, interfaces from the model |
| `reasonrd check [--config engine.yaml]`; `reasonr-build [-f components.yaml]` | The engine configuration against the models: the base one and with extensions (§31.1); a server with its own set of adapters, built from a manifest |
| `reasonrd migrate [--run <migration>]` | Migrations: the store schema by the model, manual migrations |
| `reasonrd pii rotate [--from <engine.yaml>]` | Rewrap tenant keys with a new KMS master key (`--from` — a configuration with the previous `crypto.kms`; without it — with a new key version of the same KMS) |
| `reasonrd pii replay --since <time>` | After restoring the main database from a backup, repeat the `forget`s from the `crypto.forget_log` journal written no earlier than the backup time |
| `reasonr feature list [--format json]`, `reasonr feature remove <flag> [--off]` | Feature flags of the sources (§23.4); removing a rolled-out flag from the code: the "on" branch stays (`--off` — the "off" one) |
| `reasonrd feature list\|on\|off [--config engine.yaml]` | Feature flags in production: the code's flags with date and rollout; switching on and off by level — actor, role in a tenant, tenant, everyone |
| `reasonr graph` | Mermaid diagrams of commands, events, calls, timers and external systems — an Event Storming board from code; `--contexts` — the context map with ACLs |
| `reasonr explain Order.Cancel` | A natural-language description of a command: who may call it, what it checks, what it changes |
| `reasonr pii` | The personal data map: which fields, whose, where they flow |
| `reasonr extract Context` | An analysis of extracting a bounded context into a separate process (§23.2) |
| `reasonrd perf smoke\|load\|capacity [--rate n/s] [--duration d] [--ramp d] [--config \| --url] [--baseline report.json] [--format json]` | Performance tests `*.perf.rr` (§27.6): an open load model, latencies, `then` checks over `stats` and `baseline` |
| `reasonrd demo list`, `reasonrd demo load "<scenario>" --tenant demo (--config \| --url) [--repeat n --parallel n --seed n]` | Demo data (§27.5): `*.demo.rr` scenarios fill a tenant with `demo: true` |
| `reasonrd mcp --stdio --as …` | The model's MCP server for an agent on a developer machine (§31.3.2) |
| `reasonr api publish\|unpublish\|release\|deprecate\|retire\|openapi\|list` | The external API contract (§31.3.1): publishing, versions, deprecation, OpenAPI |
| The LSP server | Highlighting, completion, go to definition, diagnostics |
| `AGENTS.md` and templates | Rules for AI agents; the loop: edit → `reasonr check` → `reasonrd test` |

**Work by role** (examples — the pilot repositories' READMEs). A product engineer works in the model: `reasonr check`, `reasonr test` (one test — `file:line`), `reasonr dev` — the model on the platform image with a real database, its own primitives and fakes. An integrator debugs a service on `reasonrd dev --integrations System=url`: the named systems run on the service, the rest are fakes. A UI developer — `reasonr-ui test` and `reasonr-ui dev` next to `reasonrd dev`. The pipeline — `reasonr test --driver api,web`.

`reasonr` commands are the compiler: they need no engine. Tests, configuration checks and building a server are done by the engine (`reasonrd`): a custom engine build runs the tests on the adapters the model runs on.

---

## 34. Safety guarantees for "vibe coding"

Mistakes a product engineer or an agent **cannot** make at the model level:

| A mistake in ordinary code | Why ReasonR does not have it |
|---|---|
| A partially applied operation on several objects | With errors the effects are not applied; the engine ensures it |
| An external call (charging money) before commit and a rollback after it | External requests leave only after commit |
| Changing state bypassing the logic | Fields change only in events |
| `NullPointerException`, accessing an object that does not exist | No `null`; `[x]?.cmd()` is safe; the compiler checks optional values |
| A mixed-up argument or identifier | Arguments are named only, id types are nominal |
| Duplicates (two accounts for one phone) | Uniqueness is declared in the model and enforced by the engine |
| Personal data in the clear or in logs | `personal` + leak control at compile time + encryption in the engine |
| Being unable to "forget" a user | `forget` is crypto-shredding everywhere, events and backups included |
| A full table scan | `where` and `sort` only by declared indexes |
| Dumping a million rows | Only by pages or `limit` ≤ 100 |
| Leaking internal state | Only value objects from queries go out |
| A customization for one client broke the others | Extensions are isolated from each other, base tests run for each extension |
| Infinite recursion | Recursion of one command is forbidden |
| A public method without authorization | Visibility is set by the name case, the compiler checks access |

What remains are **logic** errors in business rules. Against them — readability, acceptance tests at every level, `explain` and review.

---

## 35. Alternatives considered

| Alternative | Why it does not fit |
|---|---|
| Plain YAML or JSON with expressions in strings | No expression typing, poor errors, awkward to write logic |
| BPMN / DMN, low-code | Poorly versioned and reviewed in git, awkward for AI agents |
| Frameworks in Go, Kotlin, C# (Axon, EventFlow) | A product engineer cannot write and review such code; technology leaks into the logic |
| Context Mapper (CML) | Describes structure, not executable logic |
| Embedded languages (Lua, Starlark, CEL) | No domain concepts; Lua and Starlark are dynamically typed |
| A general-purpose language + linters | The restrictions can be bypassed; no guarantees "by construction" |
| Customization through dynamic fields (EAV) | Typing and static checks are lost; extensions in code give the same capabilities with guarantees |

---

## 36. Risks

| Risk | Mitigation |
|---|---|
| The language turns out not expressive enough | An RFC process for language changes; everything extra goes to external services |
| Engine complexity | A narrow `Env` interface, two implementations from day one, engine compatibility tests |
| Performance of generic storage | Typed index key columns, indexes from the model, benchmarks, compilation to Go |
| Brittle UI tests | Stable labels from the model instead of CSS selectors |
| The layout language grows into a general-purpose one | A layout has only primitives, bindings, labels, `if` and transitions: no styles, loops or expressions; anything new is a new primitive |
| The primitive catalog cannot keep up with product engineers | The automatic UI in place of a primitive not yet built; an order is a primitive declaration that already compiles |
| Extension sprawl | Additions and rules only; explicit `exclude test` with a reason is visible in review |
| Cryptography mistakes | Encryption only in the engine, standard primitives, a KMS; the model only says "whose data" |

---

## 37. Decisions and open questions

### 37.1. Decided

| Question | Decision |
|---|---|
| The name of the "after events" snapshot | `after` |
| Reading entity fields from outside | Only through the entity's own queries, which return value objects |
| Entity access | `[x]` → `T?`; `[x]?.cmd()` is safe; `[Type]` is a search |
| Entity existence | It exists if at least one event has happened to it |
| Zero identifiers | There are none: id types have no default, id fields are optional |
| Uniqueness | `index ... unique <error>`, enforced by the engine |
| Timers | `schedule` returns a `Timer.Id`, deletion by id |
| Bulk projection updates | Not done |
| Incoming events of other bounded contexts | A public command call with a role |
| Processes and sagas between bounded contexts | Entities, timers and commands are enough |
| Reentrancy | Allowed; a nested call sees the current state |
| Navigation in UI tests | The direct screen address by default, `--navigate path` — from the menu |
| A UI place for a test step | A test does not know about places: the runner goes through all applicable places in k runs (§27.3); there is no `via` |
| Setup in UI tests | `given` goes through the API, only `when` and `then` through the UI |
| UI levels | The automatic UI, a layout of primitives, an own primitive — one mechanism (§22.1) |
| Typing primitives | Type variables with constraints (`<R, K id>`) and variants `A \| B` — only in primitive declarations; the compiler infers them in a layout (§22.7) |
| Interface texts | A layout string is a label in the app's base language; model texts and translations are `texts` blocks checked by the compiler; `##` is documentation only |
| Restricting access to individual records in the model (besides `public(<field>)` and checks in queries) | Maybe later |
| Localization | In external files |
| API versioning | Not done for now |
| Extending the standard library | Only through a PR to the language code |
| Storage, GC, journals, retention, keys | Decided entirely by the engine |
| Personal data | `personal` + `forget`, leak control at compile time |
| Multitenancy | Isolation in the engine; customization through `extend`, `on`, `rule` |
| Implicit creation | The norm: `[x].cmd()` on an entity that does not exist creates it; existence is checked in commands (`if [id]`), and `?.` is used when the entity must already exist |
| Errors | Any error cancels the whole unit of work; entities react to each other with commands, not errors |
| The projection identifier | An own type `OrderMart.Id`, access `[martId]` |
| Dynamic extension fields without a deploy | Not needed |
| The order of `on` reactions | No explicit order needed |
| `decimal` precision | Fixed point: 18 digits after the point, up to 38 digits; `*` and `/` round half to even, `round` — away from zero |
| Iteration order of `set` and `map` | Insertion order; equality does not depend on order |
| Execution limits | Set by the engine; exceeding one is an execution fault; the value memory of a unit of work included (64 MB by default) |
| Strictness of `then` checks | Errors — exact, notifications and external requests — "contains" |
| Where access is checked | In the interpreter, at the start of a call from outside; the engine only authenticates |
| Value memory | The unit of work arena is shared by the interpreter and the engine; a result is valid until `Release` |
| The unit of work of a migration | Each command call from the migration |

### 37.2. Open

| Question | What needs deciding |
|---|---|
| Ordering an integration | How a product engineer orders an external system implementation from integration developers: the `external` declaration and a fake (§18.1) as the order specification, how the model and tests work while there is no implementation, accepting the finished integration |
| Ordering from engine developers | How a product engineer orders a new store or a solution to a problem the current language and engine cannot solve: where the need is recorded, how the model works until it is solved |

---

## 39. Glossary

| Term | Meaning |
|---|---|
| **Model** | A ReasonR program — a description of the business logic of a bounded context |
| **Engine** | The implementation of everything technological: storage, commit, delivery, the API, UI, encryption, tenants |
| **Interpreter** | The core that runs the model; a pure function "command → unit of work" |
| **Env** | The interface through which the interpreter gets data from the engine (`Load`, `Find`, `NewID`) |
| **SPI** | Service Provider Interface — an interface the provider (the engine) **implements** and the core calls, unlike an API the core provides. Here the SPI is `Env` |
| **Unit of work (UoW)** | All the effects of handling one external command; applied together or not at all |
| **Existence** | An entity exists if at least one event has happened to it |
| **Snapshot** | The immutable state of an entity when a command starts |
| **after** | The entity state with the events the command has already raised |
| **Actor** | An entity that is a user of the system |
| **Subject** | The entity that owns personal data; each subject has its own key |
| **Crypto-shredding** | Deleting data by destroying its encryption key |
| **Tenant** | A client of a multitenant system with its own isolated data; served by the base model or a model with an extension |
| **Extension** | A customization for a customer: `extend`, `on`, `rule` and its own declarations on top of the base model, directory `extensions/<name>/` |
| **Rule** | A pure function with default behavior that an extension may override |
| **App** | A frontend over the model: who signs in, the menu, screens |
| **Screen** | A layout of primitives bound to queries and commands |
| **Texts** | Interface labels: layout strings and `texts` blocks per language |
| **Place** | A layout node bound to a query or command; UI tests go through places |
| **Product engineer** | The author of a whole feature: the model, the interface from primitives, acceptance tests; knows only ReasonR |
| **UI primitive** | A display pattern or a control: declared in the model, implemented on the platform by UI developers |
