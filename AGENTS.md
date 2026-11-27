# AGENTS.md — how an AI agent works with a ReasonR model

A ReasonR model is the `*.rr` files in a module directory (the project sources, `src`), laid out in parts (RFC §23): bounded contexts `contexts/<Context>/` — the model, ACLs of other bounded contexts (`*.acl.rr`), screens, tests, fakes, texts; apps `apps/<App>/` — one `app` named after the directory; primitives `primitives/`; extensions `extensions/<name>/`. A declaration outside its part is E1404, a file outside the parts is E1401. A module without `contexts/`, `apps/` and `primitives/` is flat: `*.rr` files at the root.

A bounded context is a one-level namespace and the owner of its model (RFC §23.1): its declaration is named `Ordering.Order`, `Ordering.Money`; a short name is looked up in its own bounded context; roles are shared by the project (`admin`). An identifier, type or method of a bounded context works only inside it: another bounded context sees its model only in its own ACL (E1407). An ACL (RFC §23.2) is a `*.acl.rr` file in the directory of the receiving bounded context, `acl Ordering`: reactions `on Ordering.Order.placed` (the body sees the event arguments and the source `id`) and `actor Auth.User as Customer`; an identifier is translated as `Trip.Id(id)`, values with a constructor of the own type, enumerations as `CarClass(requestedClass)`. Apps and tests see the whole project.

The top level of a bounded context holds aggregates (`aggregate Order`, RFC §10; there is no top-level `entity` — E1419, the fix replaces the word) and actors. An inner entity of an aggregate is `entity Order.Stop` (RFC §10.7), usually in its own file: only private commands, events and errors, no queries or indexes (E1421); it is addressed only inside the aggregate — `[stop]`, `for s in [Stop]`, `[new Stop.Id].add(…)` (E1422), other aggregates do not keep its identifier (E1423); it is stored in the aggregate document. An aggregate with `id uuid` or `id sequence` is created by a create command `command(…) create Open()` — without an identifier, the engine issues it (E1424); in tests — `[a1].Open() as c1`. A large aggregate may be declared in parts across several files of its bounded context. The language is described in [RFC-0001](docs/rfc/0001-reasonr-language/README.md), the examples are in [examples/](examples/).

## The work loop

1. Edit the `*.rr` files.
2. `reasonr fmt -w <module>` — bring the text to the canonical form.
3. `reasonr check --format json <module>` — get the diagnostics as JSON (the format is below). The base model and the models of every extension are checked; only one — `--extension <name>` (`base` is the base model).
4. For each error:
   - if the diagnostic has `fixes` with `edits` — apply the edits as they are;
   - otherwise read `reasonr diag explain <code>`: it has an explanation, an example of the error and the fix.
5. Repeat 2–4 until `reasonr check` exits with code 0.
6. `reasonrd test --format json <module>` (in a project with `reasonr.yaml` — `reasonr test --format json`: on the project's engine) — run the acceptance tests (`*.test.rr`; the base model and every extension, `--extension <name>` — one model). The report format and how to read failures are in the engine's [AGENTS.md](https://github.com/tycheg/reasonr-engine/blob/main/AGENTS.md). A failure is either a mistake in the model logic or a wrong expectation of the test: read `message`, and if needed rerun that test with `-v` (`reasonrd test -v <file>:<line>`) to see the steps, errors, notifications and external requests of each step.
   While you edit the model, run only the affected tests: `reasonrd test --affected --base origin/main <module>` (in a project — `reasonr test --affected`); `reasonr impact --format json` tells which tests and why (RFC §27.4). Before a commit — a full run.
7. If the module has `api/` (the external API contract, RFC §31.3.1), `reasonr check` compares the model with the released versions: mark a renamed published method, parameter, field or value with the former name `was` (E1104 offers a fix), give a new parameter a default value (E1105). Remove published things only with `reasonr api deprecate … --sunset` and after that date.
8. Before shipping a new version compare it with the previous one: `reasonr check --against <schema.json> <module>` (`reasonr build --dump-schema` gives the schema of the previous version). E11xx errors mean the new version will not read the stored data without a manual migration.

Warnings (`severity: "warning"`) do not block the build, but almost always point at a logic mistake: check each one.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | no errors (warnings are possible) |
| 1 | the model has errors |
| 2 | a fault: wrong flags, no files, an I/O or compiler error |

## The JSON format of diagnostics (version 1)

`reasonr check --format json` prints one object:

```json
{
  "version": 1,
  "diagnostics": [
    {
      "code": "E0504",
      "severity": "error",
      "file": "taxi/contexts/Ordering/order.rr",
      "range": { "start": { "line": 412, "column": 7 }, "end": { "line": 415, "column": 60 } },
      "message": "no index to search Order by: add index byCustomer(customer, placedAt desc)",
      "notes": [
        { "message": "declared here", "file": "taxi/contexts/Ordering/order.rr", "range": { "start": { "line": 3, "column": 3 }, "end": { "line": 3, "column": 9 } } }
      ],
      "fixes": [
        {
          "message": "add index byCustomer(customer, placedAt desc)",
          "edits": [
            { "file": "taxi/contexts/Ordering/order.rr", "range": { "start": { "line": 18, "column": 1 }, "end": { "line": 18, "column": 1 } },
              "text": "  index byCustomer(customer, placedAt desc)\n" }
          ]
        }
      ]
    }
  ]
}
```

| Field | Type | Description |
|---|---|---|
| `version` | number | The format version. It changes on incompatible changes |
| `diagnostics` | array | Diagnostics sorted by file and position; an empty array if there are none |
| `code` | string | The code: `E` — an error, `W` — a warning, then 4 digits. The explanation is `reasonr diag explain <code>` |
| `severity` | string | `"error"` or `"warning"` |
| `file` | string | The file path relative to the directory passed to the command |
| `range` | object | The place: `start` and `end` (the end is exclusive). `line` and `column` start at 1; a column counts Unicode characters, not bytes. May be absent if the place is not known |
| `message` | string | The message |
| `notes` | array | Notes with their own places: for example, where the original name is declared |
| `fixes` | array | Fixes. `message` says what the fix does. `edits` are text edits: replace `range` in `file` with `text`. A fix without `edits` is a hint: apply it by hand |

The edits of one fix apply together; their places do not overlap. After applying, run `reasonr check` again: the positions of the other diagnostics may have shifted.

## Modules

A module (RFC §23.3) is bounded contexts, apps and primitives embedded into a project: `requires` in `reasonr.yaml` (path: version), `replace` — a local directory. `reasonr mod download` downloads modules into the cache (read-only) and writes `reasonr.lock`; `reasonr mod verify` checks the cache. Do not edit a module's files: translate its model into your own in your bounded context's ACL or extend it with an extension. A module user becomes an actor of the project through an ACL: `actor Auth.User as Customer` — a customer with the same identifier.

## Tools for understanding a model

| Command | What it gives |
|---|---|
| `reasonr explain Order.Cancel` | A method description: access, checks with errors, actions in order, who calls it; `reasonr explain Order` — the whole type |
| `reasonr graph [--entity Order] [--format mermaid\|dot]` | A diagram of commands, events, calls, timers, external requests and projections; ACL reactions — "Dispatch: Ordering.Order.on placed" |
| `reasonr graph --contexts` | The context map: ACLs from the source to the receiver with events and actors |
| `reasonr api list` | The external API contract (RFC §31.3.1): published methods, versions, deprecated ones |
| `reasonr extract Dispatch` | An analysis of extracting a bounded context into a separate process: the ACL reactions across its boundary, their commands and the errors that will no longer cancel the source command |
| `reasonr pii [--format json]` | The personal data map: fields and parameters by subject, where values go, where a subject is forgotten |
| `reasonr diag list` | Every diagnostic code with a short title |
| `reasonr build --dump-schema` | The model schema as JSON — what the engine sees |
| `reasonr impact [--base origin/main] [--format json]` | Which acceptance tests a change affects and through which functions; `reasonr build --dump-impact` — the graph |
| `reasonr build --dump-ir` | The intermediate representation — for debugging the compiler |
| `reasonr build --dump-bytecode` | The bytecode the interpreter runs — for debugging |

## Interface

Apps (`app`), screens (`screen`), primitives (`primitive`) and texts (`texts`) — RFC §22; the example is the screens and texts of `examples/taxi/src/contexts/Ordering/` (`ui.rr`, `admin.rr`, `texts.en.rr`) and the apps `examples/taxi/src/apps/`. A layout has no styles, loops or arbitrary expressions — only primitives, bindings, labels, `if` and `on` transitions. The standard catalog is `internal/std/ui.rr` of the compiler.

Binding arguments are filled implicitly (RFC §22.4):
- **An argument — by name.** First an explicit value (`(newEmail: email)`, `orderId: new`). Then the same-named value of the nearest scope: row → data → screen parameters and `me`. Otherwise — input: a filter for a query, a form field for a command. `PageRequest` is filled by the primitive.
- **The target of command `X` — by type.** An explicit `[expression]`, otherwise the only `X.Id` value in scope, otherwise `me` if `X` is the app's actor. Otherwise — a form field with the default primitive for `X.Id` (`default Customer.Id EntityPicker(…)`).
- **Fixed and initial values.** Values from parameters, `me` and `new` are not shown in the form. Values from data become the form's initial values.
- **Ambiguity and types.** Two `X.Id` values in one scope give E0705: set the target `[x]`. A same-named value of another type gives E0706: bind the argument explicitly.

How to read the codes:
- E07xx — the interface, the explanation is `reasonr diag explain E07xx`;
- W1301 — no one admitted to the app can access a node: check the method access and `app(…)`;
- W1302 — a screen cannot be reached;
- W1303 — a text has no translation;
- E1011 — `extend screen … in Part` is ambiguous.

Tools:
- `reasonr explain App.Screen` shows where each argument comes from;
- `reasonr build --dump-ui` prints the interface program;
- `reasonr ui eject App` prints the `app … auto` layout for editing, by project part files: `# apps/<App>/app.rr` — the app, `# contexts/<Context>/…` — screens.

Running tests through the UI (`reasonrd test --driver web`) is described in the engine's AGENTS.md.

## Feature flags

New functionality is written under a feature flag (RFC §23.4): it ships to production switched off and rolls out without a restart.

1. Add the flag in a bounded context file with today's date: `feature tips "2026-11-07"`, with a `##` description above it.
2. Write the new code under the condition: `if tips` in commands, queries, rules, `on` reactions, layout (`Edit Order.LeaveTip if tips and status == paid`). Events do not read flags (E1504): pass the value as an event parameter.
3. Flags do not nest: one flag per condition (E1503), and a branch of a condition with a flag, `else` included, has no other flag (E1502).
4. Cover the new behavior with tests that have a `feature tips` line at the start. Do not change old tests: they check the behavior with the flag off and must pass as they are. A flag without such a test is W1501; a change that adds a flag and changes an old test is rejected by `reasonr impact --base origin/main`.
5. Once the flag is rolled out to everyone, remove it: `reasonr feature remove tips` (keeps the "on" branch; `--off` — the "off" branch and removes the flag's tests), then `reasonr check` and `reasonr test`. Old tests that checked the previous behavior are fixed in the same change.

`reasonr feature list --format json` lists the flags of the sources: the date, the age, how many places read the flag, which tests switch it on. The production rollout is shown by `reasonrd feature list`.

## Demo data

A demo scenario (RFC §27.5) is a `test` in a `*.demo.rr` file with a single `given` section (E0805): loops, `faker`, `time offset -7d` at the start, the answers of external systems as commands with their roles. `reasonrd test` runs demos in memory: a step error is a failure. Filling a tenant with `demo: true` in `engine.yaml`: `reasonrd demo load "<scenario>" --tenant demo --config engine.yaml [--repeat 4 --parallel 4]`; the list — `reasonrd demo list`.

## Performance tests

A performance test (RFC §27.6) is a `test` in `*.perf.rr`: `given` fills the system once, `when` is a user path of several steps, `then` holds expressions over `stats` and `baseline` (`stats.p95 < 100ms`, `stats.of(Customer.OrderTaxi).p99 < 300ms`, `stats.errors == 0`, `stats.p95 * 10 <= baseline.p95 * 11`). Running: `reasonrd perf smoke|load|capacity [--rate 300/s] [--duration 15m] [--config engine.yaml] [--baseline previous.json] [--format json] <module>`; code 1 means a false check.

## Personal data

The model only says **whose** data it is (RFC §20); the engine handles keys and encryption.

- Mark a field `personal`, and the parameters through which the value reaches it: a command parameter, an event parameter, a projection field (`personal(subject_field)`), an external request parameter. `reasonr pii` shows where values flow and where a subject is forgotten.
- Personal fields and event parameters are optional (`T?`). After `forget` they read as `none`: the code and the tests must handle that.
- Indexes on a personal field are equality only, unique ones included.
- `forget id` in a command destroys the subject key and erases its data if the unit of work has no errors. In an acceptance test, after `Forget` check that the fields became `none`, that another subject may take a freed unique value and that signing up the same subject again works.
- The `personal` marking of a field with data may change: the engine encrypts or decrypts the values itself on `reasonrd migrate`. Key settings (`crypto` in `engine.yaml`), the `forget` journal and restoring from a backup are described in the engine's README.

## External systems and fakes

- A system answers with a public command with its role: `external(matching) Matching`, the answering command is `command(matching) DriverFound(…)`.
- A request deadline is a model timer: a `timeout Timer.Id` parameter; the model sets the timer with `schedule` and passes it to the request, and the timer command is the reaction to "the answer did not come".
- The behavior of a system in tests is described by a fake `fake Matching` in a `*.fake.rr` file (RFC §18.1): fields, events, setup commands and request implementations that call public model commands. A fake sees only the request arguments and its own fields.
- In a test, a fake is set up with a command without `as`: `Matching.Setup(freeDrivers: [d1])` in `given`. Until the fake is created, requests are only recorded, and the test calls the answers itself (`[o1].DriverFound(…) as matching`).
- A `fake` failure in the `reasonrd test` report means a command the fake called returned errors: check the model or the fake.

## Rules most often broken

- The top level holds `aggregate`, not `entity` (E1419); an event of another bounded context is handled by an ACL, not by an aggregate (E1415).
- Entity fields change only in events; a command raises an event (E0301, E0302).
- Arguments are passed by name: `placed(orderId: id)` (E0010).
- Fields, parameters and variables have different names (E0103).
- A `T?` value is checked before use: `if x is not none`, `x?.field`, `x ?? fallback` (E0219).
- A search goes only by an index, with `page` or `limit ≤ 100`, and the result is a value object (E0501–E0510).
- Personal data goes only to places marked `personal` with the same subject (E0901–E0905).
- An error does not stop a command: after the checks write `if hasErrors` / `return`.
- A projection row is written whole: pass every field except `T?` and fields with `= value` (E0305). One entity writes it; data of other entities comes to it as command arguments. An extension does not extend projections (E1010).
- The fields of the query's value object from which a command form takes its initial values are named like the command parameters (`OrderDetails.orderRoute` — `Order.Edit(orderRoute)`); otherwise bind explicitly `(newEmail: email)` (E0706, E0708).
