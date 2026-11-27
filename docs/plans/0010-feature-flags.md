# Plan: feature flags

| | |
|---|---|
| **Basis** | [Roadmap](../roadmap.md), item 2; the author's requirements of 2026-11-07 (§1) |
| **Status** | The decisions of §2 agreed 2026-11-07 (the third version: the author's answers to the questions — §4). F0–F5 done 2026-11-08. Done |
| **Branch** | `feature-flags` |
| **Note** | Paths and RFC section numbers are those of the time (the repository `tycheg/reasonr` before the split). Feature flags of the model; flags of the framework itself are a separate later item |

---

## 1. Process

1. A product engineer creates a flag and records its creation date in the code.
2. They write the new functionality under the flag: part of a command, part of a query, screens and so on. They cover the new functionality with tests that turn the flag on. The old tests do not change and pass: changing them is forbidden.
3. The new functionality is rolled out to production with the flag off.
4. The engineer rolls the flag out: first to one user (themselves), then to a role in a tenant, then to a tenant, then to the whole system. If something goes wrong, they turn the flag off.
5. Once the flag is rolled out to the whole system, the engineer removes the flag from the code.

In production one can see which flags exist, when they were created and whom they are on for.

| Step | How |
|---|---|
| Create a flag | `feature tips "2026-11-07"` in a file of the system's bounded context |
| Code under the flag | `if tips` in commands, queries, rules, reactions, the layout of screens and apps, extensions |
| A test of the new functionality | a line `feature tips` at the start of the test |
| Rolling out and turning off | `reasonrd feature on tips --actor Urcar.Customer:… \| --role Urcar.admin --tenant acme-prod \| --tenant acme-prod \| --all`, `reasonrd feature off tips` |
| The list in production | `reasonrd feature list`: the flag, the system, the creation date, the description, whom it is on for |
| Remove the flag from the code | `reasonr feature remove tips`: the "on" branch stays |

## 2. Decisions (proposal)

| # | Decision |
|---|---|
| 1 | **Declaration.** `feature <name> "<creation date>"` — a top-level declaration in a bounded context file, with a `##` description above it. A flag belongs to a system, like a role (RFC §23.1): its full name is `Urcar.tips`, in its own system `tips`, from another system `Auth.newLogin`. A flag's name is unique in the system. Extensions and modules may declare flags too. The date is a string strictly of the form `2026-11-07` (`YYYY-MM-DD`, an existing date); otherwise a compile error |
| 2 | **A flag is a boolean value.** It can be read wherever code or a layout runs: in commands, queries, rules, `on` reactions, the layout of screens and apps (`if tips`), extension code. A flag cannot be read in events: replaying events must not depend on it, just as with rules (RFC §21.2) |
| 3 | **Nesting flags is a compile error.** An error is: a condition with a flag inside a branch (`if` or `else`) of a condition with a flag, the same one included; two flags in one condition. The check is lexical: calling a command from a flag's branch is not an error if the command has its own flag |
| 4 | **The value is per unit of work.** The engine computes the set of flags once, for the caller of the root command or query. The same set holds in nested calls and reactions: the branches of one unit of work do not diverge. The set is passed to the interpreter with the call (a bit set in `Caller`, without allocations) |
| 5 | **Rollout levels:** an actor (`Urcar.Customer:<id>`, as in the `Test actor=` header), a role in a tenant, a tenant, the whole system. A flag is on for a caller if it is on at at least one of their levels. `off` turns the flag off at every level at once. A level can be removed separately too: `off --actor …`. For a caller without an actor (a timer, an external system's response, a migration) the role, tenant and system levels apply — nothing extra is remembered; if a timer's command needs the flag's value at scheduling time, the model passes it as a parameter itself |
| 6 | **The flags' state is kept in the engine's store**, not in `engine.yaml`: turning a flag on needs no restart. Engines see changes within a few seconds: a cache plus a notification through `notifications.hub`. Every change is written to a log: who, when, what |
| 7 | **Schema and data.** There is one program, and the code under a flag is always in it. The fields, events and commands of the new functionality are in the store's schema, so turning a flag on needs no migration. The schema lists the flags with their dates — `reasonrd feature list` shows the code's flags from it |
| 8 | **Tests.** A line `feature tips[, …]` at the very start of a test turns the flags on for the whole system for that test; in tests without it every flag is off. The old tests check the behavior with the flags off and pass unchanged. A flag without a test that turns it on is a warning. Tests with a flag run on every driver: `memory`, `api`, `web` |
| 9 | **UI.** The app API returns the current user's set of flags, and the runtime computes `if tips` in the layout. UI places under a flag that is off are hidden, and `--driver web` sees that |
| 10 | **The list in production.** `reasonrd feature list [--format json]` with the engine configuration shows the code's flags (the system, the name, the date, the age, the description) and their rollout. Flags left in the store but removed from the code are marked. `reasonr feature list` shows the sources' flags without state |
| 11 | **Removal.** `reasonr feature remove tips` keeps the "on" branch: it removes the conditions with the flag, the `else` branches, the declaration and the `feature tips` lines in tests. With `--off` the "off" branch stays (the feature was dropped), and the tool removes and lists the tests that turned the flag on. If old tests checked the previous behavior, they are fixed in the commit that removes the flag: the only moment an old test changes |

## 3. Syntax (a sketch)

```rr
# contexts/Urcar.Ordering/order.rr
## A tip for the driver after the ride
feature tips "2026-11-07"

entity Order
  tip Money?

  command(customer) LeaveTip(amount Money)
    if tips
      ...                     # part of the command under the flag

  query(customer) Details() OrderDetails
    ...
    if tips
      ...                     # part of the query under the flag
```

```rr
screen MyOrder(orderId Order.Id)
  ...
  if tips
    Edit Order.LeaveTip
```

```rr
test "The customer leaves a tip"
  feature tips
  given
    ...
```

## 4. Postponed

The author's answers of 2026-11-07:

| Question | Decision |
|---|---|
| A whole new command, query or screen under a flag (a flag on a declaration) | Nothing for now: we watch how a flag works in code and layouts. A new command is visible in the API, and an `if` hides its place in the UI |
| Who the "user" is in a rollout | An actor (decision 5) |
| Flags for a caller without an actor | Nothing extra (decision 5): a flag is passed into a timer as a parameter by hand |
| The ban on changing old tests | Not checked for now; the check will be part of test impact analysis (the roadmap, item 6) |

## 5. Stages

| Stage | Result |
|---|---|
| **F0. RFC** | A section "Feature flags": the declaration, the value and the unit of work, nesting, rollout levels, tests, UI, the lifecycle; the grammar; §27 (the `feature` line in a test), §33 (tools); codes with explanations |
| **F1. The compiler** | The `feature` declaration with a date check and a lookup by system; a flag as a boolean expression; errors of nesting and of reading a flag in events; the `feature` line in a test; the warning "a flag without a test"; flags in the schema and the bytecode (reading a bit from `Caller`); `fmt`, `explain`, `graph` |
| **F2. The interpreter and the engine** | `Caller.Features`; the flag state port and the memory, PostgreSQL, MongoDB adapters; computing the set per unit of work, a cache and notifications; the change log; `reasonrd feature list\|on\|off`; a test API for turning flags on; `reasonrd test` on `memory` and `api`; `reasonrd dev` |
| **F3. UI** | Flags in the app API, `if` with a flag in the runtime and the auto UI, `--driver web` |
| **F4. Tools** | `reasonr feature list\|remove`; the LSP (go to definition, a hover with the date, highlighting), VS Code |
| **F5. The example and documents** | Taxi: the flag `tips` — a tip for the driver (a command, part of a query, a screen, a test); the earlier tests untouched; the README, AGENTS.md (the work loop with a flag), the example's README |

The acceptance criterion:
- taxi passes every test on `memory`, `api` and `web`;
- in `reasonr dev` the flag `tips` is rolled out step by step: actor → role in a tenant → tenant → system;
- after `off` there are no tips either in the UI or in the API;
- `reasonrd feature list` shows the flag with its date and rollout;
- after `reasonr feature remove tips` `check` and `test` pass.

## 6. Progress

| Stage | Status | What was done |
|---|---|---|
| F0 | Done | RFC §23.3, the grammar, §27.1, §28.3, §30.2, §30.5, §31.1, §33; the codes E1501–E1506, W1501 with explanations |
| F1 | Done | `feature <name> "YYYY-MM-DD"`, a flag is a `bool` of the system (`tips`, `Auth.x`), E1502/E1503 (lexically, and in layouts), E1504 (events, value methods, migrations, fakes, test `let`s), the `feature` line in a test, W1501, `Schema.Features`, `Caller.Features` (`[4]uint64`, up to 256 flags), the bytecode `feature`/`test.feature`, explain and graph |
| F2 | Done | `port.FeatureStore`/`FeatureHub`, the memory, PostgreSQL (`rt_features`, `rt_feature_log`, NOTIFY), MongoDB adapters; `engine/features` (0 allocations, a cache, rereading every 5 s); `features` in engine.yaml; `reasonrd feature list\|on\|off\|log` (`--config`, or `--url` for `reasonrd dev`); `/_test/{tenant}/features/{flag}`; the drivers memory, api, web. Flags are off in migrations |
| F3 | Done | The app API computes `if` with the caller's flags; the manifest has the flags that are on and the visibility of menu nodes |
| F4 | Done | `reasonr feature list\|remove [--off]`; the LSP: go to definition, hover, highlighting, completion; VS Code |
| F5 | Done | Taxi: the flag `Urcar.tips` — the command `LeaveTip`, part of the query `Card`, a place in `MyOrder`, tests with the line `feature tips`; the earlier tests untouched; the tests pass on `memory`, `api`, `web`; the rollout actor → role in a tenant → tenant → system in `reasonrd dev`; after `reasonr feature remove tips` `check` and `test` pass; the README, AGENTS.md, the taxi README, an LSP test |
