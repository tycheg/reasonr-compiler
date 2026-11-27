# Plan: modularity — systems, bounded contexts, apps, extensions, modules

| | |
|---|---|
| **Basis** | [Roadmap](../roadmap.md), item 1; a discussion of 2026-11-07 |
| **Status** | The decisions of §2 agreed 2026-11-07. M0 is done (branch `extensions`), M1 — branch `project-structure`, M2 and M4 — `systems-contexts`, M3 — `apps-primitives`, M5 — `modules` (§5). Done |
| **Date** | 2026-11-07 |
| **Note** | Paths and RFC section numbers are those of the time (the repository `tycheg/reasonr` before the split). Systems were later removed: bounded contexts became the top level ([plan 0011](0011-contexts-acl-aggregates.md)) |

---

## 1. Notions

| Notion | What it is | Boundary |
|---|---|---|
| **Project** | A software product in ReasonR: `reasonr.yaml` + `src/`. One program: the systems of the project and of the modules it uses compile together, and the unit of work and the store are shared | Systems, apps, primitives, extensions, module dependencies |
| **System** | A first-level namespace, like a namespace in C#. The boundary of entity identity: `Order` in two bounded contexts of a system is one real-world entity with one `Order.Id`; `Taxi.Order` and `Grocery.Order` are different entities (a taxi order and a shop order) | The names of bounded contexts and entities |
| **Bounded context** | A second-level namespace: the models of the system's entities (their own fields, events, commands, queries), values, enums, rules, screens, texts, tests, fakes of external systems. A bounded context without entities is a shared kernel; there are no separate libraries | Private commands and queries, fields, values and enums |
| **App** | `app`: who enters, a menu of screens of the bounded contexts of any system of the project | The platform (web, mobile, desktop) comes from a UI module |
| **Primitive** | A display pattern or a control shared by the whole project | Declarations — `src/primitives`, implementations — the platforms' UI modules |
| **Extension** | Customization for a client: `extend`, `on`, `rule` and own declarations on top of the project. A **tenant** is only a client with their own isolated data; a data tenant picks an extension in `engine.yaml` | The structure mirrors the top level of the project |
| **Module** | A unit of distribution embedded in a project: one or more systems, Go adapters of the engine, primitive implementations, integration services. An example — authorization through Keycloak: the system `Auth`, the engine's authentication adapter, sign-in screens | A version, dependencies |

---

## 2. Decisions

| # | Decision |
|---|---|
| 1 | **The term is "system"** (`System`): "domain" does not fit, a bounded context may span several domains |
| 2 | **Bounded context and system names are capitalized**, like type names: `Taxi`, `Ordering`, `Billing`. A type cannot be named like a bounded context or a system |
| 3 | **Full names use dots**, as in C#: `Taxi.Ordering.Money` — a type of a bounded context, `Taxi.Order` — an entity of the system (the bounded context is not written: the entity belongs to the system, and its public names are unique in the system, decision 12). Short names: in one's own bounded context — `Money`, from another bounded context of one's own system — `Billing.Money`, `Order`; from another system — `Auth.User.Id`, `Auth.Accounts.Credentials` |
| 4 | **An entity belongs to a system, its model to a bounded context.** A bounded context has one model of an entity; fields, private commands and queries — only of its own bounded context. Public names (commands, queries) are unique across the whole entity in the system, not per bounded context |
| 5 | **Visibility.** Inside a bounded context — everything, as now inside a module. To the whole project — other bounded contexts and other systems — public commands and queries. Private commands and queries are not available outside the bounded context |
| 6 | **The unit of work and the store are shared by the project**: calling a public command of another bounded context or another system is synchronous, in the same unit of work; so are `on` reactions to events of another system. Systems of one project do not interact asynchronously and are not replaced by fakes. A bounded context's own store and unit of work — later, at the end of the roadmap (item 25) |
| 7 | **An actor is an entity of a system**: one `Id`, different attributes in bounded contexts; `me` works for the actor in every bounded context |
| 8 | **Screens are in bounded contexts**, apps are above the bounded contexts of any system of the project, primitives are in `src/primitives`, shared by the project |
| 9 | **A test lives in a bounded context** next to the entity the `when` action belongs to, and may call the whole project |
| 10 | **Another system** has its own identity: authorization lives in the system `Auth` (from a module), and taxi refers to `Auth.User.Id` and calls its public commands directly |
| 11 | **Customization for a client is an "extension"** everywhere: the directory, flags, reports, `engine.yaml`, texts (stage M0) |
| 12 | **Unique across the whole entity in a system** are public commands and queries, events (`on` reactions from other bounded contexts react to them), errors and notifications of public commands (the client sees them); fields, private commands and queries — per bounded context |
| 13 | **An entity's model exists in a bounded context** from the first event of its bounded context (RFC §10.3 — per bounded context) |
| 14 | **A reaction to an event of another bounded context** — `on Ordering.Order.placed`, in the same unit of work, as with an extension |
| 15 | **A user of another system becomes an actor** through a field with a unique index (`user Auth.User.Id`) and a link in the engine's authentication (the authorization module's adapter) — details in M5 |
| 16 | **A bounded context's directory is `src/contexts/<System>.<Context>`** (`Taxi.Ordering`, `Grocery.Ordering`): the full name of the bounded context, capitalized, without transformations; there is no separate system directory |
| 17 | **A system's name does not match a bounded context's name** (of one's own systems and of module systems) — a compiler error |
| 18 | **A role belongs to a system**; from another system — with a prefix: `command(Auth.admin)` |
| 19 | **A project adapts a module** with its own bounded context in the module's system (`src/contexts/Auth.Profile` — its own model of `Auth.User` with the same `Id`); rules and reactions — by an extension |
| 20 | **Module sources are in the module cache**, as in Go: read-only; the version is in `reasonr.yaml` and a lock file |
| 21 | **The project structure:** |

```
model/
  reasonr.yaml                   # src, platform — as now; requires — modules
  src/
    contexts/
      Taxi.Ordering/   order.rr customer.rr order.test.rr matching.fake.rr texts.en.rr
      Taxi.Dispatch/   driver.rr dispatch.rr driver.test.rr
      Auth.Profile/    user.rr                # one's own bounded context in the module's system Auth
    apps/
      Customer/   app.rr texts.en.rr
      Admin/      app.rr
    primitives/   primitives.rr
    extensions/
      acme/
        contexts/Taxi.Ordering/   order.rr
        apps/Customer/            app.rr
```

The name of a bounded context (with its system), an app and an extension is the name of its directory. The system is the first part of a bounded context's name; the bounded contexts of module systems (`Auth.Accounts`) come from the dependencies.

---

## 3. Stages

| Stage | Result |
|---|---|
| **M0. Tenant → extension** | The directory `extensions/<name>/` instead of `tenants/`, without the header `tenant <name>` (the name is the directory); `--extension` instead of `--tenant` in `reasonr check|test`, `reasonrd test`; the field `extension` in the test report; `engine.yaml`: `tenants: { acme-prod: { extension: acme } }`; diagnostics, help, RFC §21, the README, AGENTS.md, the examples, VS Code highlighting. "Tenant" stays only for data |
| **M1. The project structure** | The directories `src/contexts/<System>.<Context>`, `src/apps`, `src/primitives`, `src/extensions`; loading a project by them; the examples: taxi — the system `Urcar`, bank — `ZBank`, one bounded context each |
| **M2. Systems and bounded contexts** | Namespaces and full names, the visibility of private members, entity models in several bounded contexts with one `Id`, uniqueness of an entity's public names; storing the models and their events in the engine; taxi cut into bounded contexts (`Urcar.Ordering`, `Urcar.Dispatch`) |
| **M3. Apps and primitives** | `app` in `src/apps` above the screens of bounded contexts of different systems, primitives in `src/primitives`; the web runtime and `--driver web` |
| **M4. Extensions by structure** | `extensions/<name>/contexts/<System>.<Context>`, `extensions/<name>/apps/<App>` |
| **M5. Modules** | Dependencies (`requires` in `reasonr.yaml`), versions; a module's systems compile into the project's program, its Go adapters into the platform engine, its primitives and screens into the UI; the first module is authorization through Keycloak (the roadmap, item 12) |

---

## 4. Open questions

None. The app `Dispatch` and the bounded context `Urcar.Dispatch` have the same name — that is allowed (the author's decision of 2026-11-07): apps are outside the namespaces of systems and bounded contexts. The example systems are `Urcar` (taxi) and `ZBank` (bank); the author chose the names.

---

## 5. Progress

| Stage | Status | What was done |
|---|---|---|
| M0 | Done | The directory `extensions/<name>/`, the `tenant` header and keyword removed; `--extension` in `reasonr check|build|explain|graph|pii|ui eject`, `reasonrd test`, `reasonrd openapi --integrations`; the field `extension` in the test report and in the schema (`--dump-schema`); `engine.yaml`: `tenants: { default: {}, acme-prod: { extension: acme } }`; the test API: `POST /_test/tenants {"extension": …}`; RFC §5, §21, §28, §30, §33, the glossary; the README, AGENTS.md, the examples, VS Code |
| M1 | Done | Loading by the parts of a project: `contexts/<System>.<Context>/`, `apps/<App>/`, `primitives/`, `extensions/<name>/`; where declarations may go; the codes E1401–E1405 with explanations; the LSP, `reasonrd test <file>` and `reasonr check <dir>` find the module by its parts; the examples: taxi — `Urcar.Ordering` and the apps `Admin`, `Dispatch`, `DriverApp`, `Taxi`; bank — `ZBank.Accounts`, the app `Bank`, the primitive `AccountNumber` in `primitives/`; RFC §23 "The project: parts and directories" (revision 8), the README, AGENTS.md |
| M2 | Done | Full names (`Urcar.Ordering.Money`, the entity `Urcar.Order`, the role `Urcar.admin`) and looking up a short name: one's own bounded context → the system's entities and roles → outside namespaces; the paths `Ordering.Money`, `Auth.User.Id`, `command(Auth.admin)`; visibility (E1407), entity models in bounded contexts with one `Id` (E1408–E1410), the reactions `on Ordering.Order.placed` (E1411); a type is not named like a system or a bounded context (E1406). The engine: a model is its own schema type (`entity` is the entity's name), the Id is the main model's, a public method of an entity is looked up in all its models (the API, `/openapi.json` — the paths `/api/Urcar.Order/…`), short names of types and roles in the API and in authentication, stores for names with dots, the short name of an external system in integrations. Taxi is cut into `Urcar.Ordering` (the customer, staff, the order, payment, the board) and `Urcar.Dispatch` (the driver, matching, the ride); RFC §23.1, §25, AGENTS.md |
| M3 | Done | Apps `apps/<App>/` above the screens of bounded contexts (taxi: `Dispatch` — the board from `Ordering` and the free drivers from `Dispatch`), primitives in `primitives/` (bank: `AccountNumber`); the web runtime and `--driver web` with full names of screens and types: place names use the screen's short name, the TypeScript types (`--ui-types`) are short if unique; `reasonr ui eject` lays the layout out into the files of the project's parts |
| M5 | Done | `requires` and `replace` in `reasonr.yaml` (`compiler/project`), a read-only module cache (`REASONR_MODCACHE`), loading by a git tag and `reasonr.lock` with sums (`reasonr mod download`, `reasonr mod verify`); module systems are part of the project's base model (module tests are not); modules are mounted into the platform container (`reasonr test`, `reasonr dev`: `REASONR_MODULES`); the actor link (decision 15) — the schema's `link` and the interpreter; the first module `modules/auth` — the system `Auth`, the app `Account`, the adapter `type: keycloak` (`realm_access.roles`); taxi: the customer is linked to a Keycloak user (`Join`), its own bounded context `Auth.Profile`, the app `Join` above the screens of `Urcar` and `Auth`; RFC §23.2, AGENTS.md, the README |
| M4 | Done (in M2) | `extensions/<name>/contexts/<System>.<Context>/`, `extensions/<name>/apps/<App>/`: the same parts and checks as a project's; `extend` finds the declaration of its bounded context. A flat module has flat extensions |

Deviations of M0:

1. **`--extension base`** — only the base model, as `--tenant base` before; `base` is not written in `engine.yaml`: a tenant without `extension` runs the base model.
2. **Removed diagnostics** E0019 (the header not first), E1001 (no header), E1008 (a header in a base file): without a header they are not needed.
3. **`--tenant` of `reasonrd migrate` and `reasonrd timers retry`** stayed: it is a data tenant.
4. **Plans 0001–0008** were not changed: they are a history of decisions.

Deviations and decisions of M1:

1. **The examples' bounded contexts** are `Urcar.Ordering` and `ZBank.Accounts`: one per system. The author chose the system names (2026-11-07); the bounded context names were chosen during the implementation. Taxi is split into `Ordering` and `Dispatch` in M2.
2. **A flat module stays**: a directory without `contexts/`, `apps/` and `primitives/` is one bounded context without a name, with `*.rr` files at the root. The short RFC examples, the code explanations and the compiler tests use it. Files at the root of a laid-out module, or directly in `contexts/` or `apps/`, are E1401.
3. **Where declarations go** (E1404): `app` — in `apps/`, `primitive` and top-level `default` (for every app) — in `primitives/`, `texts` — in any part, the rest — in bounded contexts. `apps/<App>/` holds one app with the directory's name (E1403).
4. **Decision 17 is checked already in M1** (E1405): by the names of the bounded context directories.
5. **The order of apps** follows the names of the `apps/` directories (before — by files): the order in the UI program changes (`--dump-ui`).
6. **Extensions are flat until M4**: `extensions/<name>/*.rr`.
7. **`reasonr ui eject`** printed the app and the screens together; in M3 it lays them out into the files of the project's parts.
8. **RFC §23** holds the section "The project: parts and directories" — the number was freed after "Namespaces and conflicts" was removed.

Deviations and decisions of M2:

1. **An entity's model is its own schema type** `Urcar.Ordering.Order` with a field `entity: Urcar.Order`: each bounded context has its own record, fields and events — so existence per bounded context (decision 13) comes for free, and a bounded context's own store (roadmap item 25) needs no data migration. The identifier of every model is the type of the main model (the first by name).
2. **Reactions to events of another bounded context run after the root command**, in event order, in the same unit of work; extension reactions — as before, after their command. Otherwise a reaction to an event of a nested command would get ahead of a reaction to an earlier event: the order status in `Ordering` would diverge from matching in `Dispatch`.
3. **A reaction `on Context.Entity.event` belongs to a model of the same entity** (E1411): the models of other entities react to their events in their own bounded context.
4. **In tests and outside bounded contexts (apps, primitives, the generated auto UI)** a short name is looked up across the whole project and must be unique: a test calls the whole project (decision 9).
5. **External systems** have names unique in the project: integrations (the paths `/requests/Matching/…`, the `integrations` keys, topics) know them by their short name. The `projections` keys in `engine.yaml` are the full or the short name of a projection.
6. **Names in the API, in `Test` headers and in tokens** — full, or short if the name is unique in the model: `/api/Order/…`, `role=admin`. Responses, the schema, the IR and reports use full names; values are printed with the type's short name (`Route(…)`), UI places with the screen's short name (`Admin.AdminOrder:Order.Edit`) if it is unique in the app.
7. **The taxi tests**: `ride.test.rr` (rides on the matching fake) — in `Urcar.Dispatch` next to `Matching`, the rest — in `Urcar.Ordering` next to the order; their `when` steps go through both bounded contexts.
8. **The Auth module and a user of another system as an actor** (decision 15) — in M5.

Deviations and decisions of M3:

1. **Auto screens** are built outside bounded contexts (`auto/<App>.rr`) and named by their short name; screens unloaded by `eject` go into the bounded context of the type they are bound to — into the bounded context of the first model if there are several.
2. **An app above systems** is checked on one system (taxi `Urcar` — the screens of `Ordering` and `Dispatch` in one app); above two systems — with the authorization module in M5.

Deviations and decisions of M5:

1. **The `modules/` directory** at the repository root — for ReasonR modules (the first is `modules/auth`); plan 0007 did not foresee it at the root.
2. **A module path is a git repository path**: `host/owner/repository[/subdirectory]`, the tag is `subdirectory/version` (as in Go). There are no published module tags yet (publishing — only on the author's command): the examples use the module through `replace`. `REASONR_GIT_BASE` replaces `https://` (tests — a local repository).
3. **Module dependencies** (a module requiring a module) are not supported yet: a module with `requires` is an error.
4. **The actor link** (decision 15): a project's actor has a unique index on one field `X.Id`, where `X` is an actor of another system. A caller `X` passes the access of this actor (`command(Customer)`, `self`) if the linked record exists; `me` is its id. The lookup is by the unique index through the engine (`Env.Find`) on every call. A user `X` does not enter `app(Customer)`: their apps are `app(X)`, and nodes with the customer's access are visible in them. Access by field (`command(customer)`) does not take the link into account.
5. **Modules in the platform container**: `reasonr test` mounts the module directories (`/modules/<n>`), `reasonr dev` mounts a copy of them into the debug environment; the engine gets `REASONR_MODULES`. The platforms' `compose.dev.yaml` have a `/modules` volume and the `REASONR_MODULES` variable.
6. **The debug database of `reasonr dev`** with a schema from before M2 is incompatible with full names (every type is renamed): the engine requires a migration. For the examples — recreate the database (`docker volume rm taxi-dev_postgres`).
