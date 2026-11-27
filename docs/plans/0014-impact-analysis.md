# Plan: test impact analysis — running only the affected tests

| | |
|---|---|
| **Basis** | [Roadmap](../roadmap.md), item 6; [plan 0010](0010-feature-flags.md), §4 (checking old tests) |
| **Status** | Implemented 2026-11-13 (§6). Done |
| **Branch** | `impact` |
| **Note** | Paths and RFC section numbers are those of the time (the repository `tycheg/reasonr` before the split) |

---

## 1. Goal

A change (a commit, a branch) runs only the acceptance tests whose result it may affect; main runs the full set. The analysis is exact by construction: a test is skipped only if it is proven that none of the code it runs and none of the interface descriptions changed. The check of a change with feature flags belongs here too: the old tests do not change.

## 2. Decisions (proposal)

| # | Decision |
|---|---|
| 1 | **The unit of change is an IR function and a schema element**, not a source line: renaming a variable, formatting and comments do not affect tests. Every IR function (a command, applying an event, a query, a rule, a method, a reaction, a check, a layout expression, a test) has a hash of its IR together with the hashes of the types it reads (the schema of fields, indexes, default values) |
| 2 | **The compiler builds a test's reachability graph**: the test's steps → public commands and queries → command calls, events and applying them, `on` reactions (in other bounded contexts too), rules, methods, projections and queries over them, timers (`schedule`), requests to external systems → fake implementations → response commands. For `--driver web` — also the step's UI places and the screens' layout expressions (`if` conditions, arguments), the error texts the driver checks. The graph is in `reasonr build --dump-impact` (JSON: test → a set of hashes) |
| 3 | **Affected tests:** a test is affected if its set holds a hash that the base version does not have (or the test function itself, the shared `let`s, the fakes, the engine configuration, the compiler/engine version changed — then a full run). The base version is `--base <git-ref>` (`git merge-base` with main): the compiler builds both versions from git without a checkout |
| 4 | **Commands:** `reasonr test --affected [--base origin/main]` and `reasonrd test --affected`; the output says which tests are skipped and why (`-v`). `reasonr impact --base main --format json` — the list of affected tests and the reasons (for CI and agents) |
| 5 | **Safety of the analysis.** Everything the analysis does not see means a full run: a change of `engine.yaml`, `reasonr.yaml`, the module dependencies (`reasonr.lock`), the UI primitive implementations (`--driver web`), the platform's Go code. The nightly run on main is always full; if a full run finds a failure the analysis skipped, that is a bug of the analysis (the test for it — §5) |
| 6 | **Extensions:** the analysis runs per model (the base and the extensions): a change in the base model affects the tests of extensions that reach the changed function |
| 7 | **Feature flags — the old tests do not change** (plan 0010, §4). `reasonr impact --base main` reports an error if a change **creates a flag** and at the same time changes a test that does not turn this flag on (the hash of the test function changed); the exception is the commit that removes the flag (`reasonr feature remove`): in it the flag goes away and the old tests may change. An allowed exception is explicit: `reasonr impact --allow-test-changes "<test>"` with the reason in the commit message |
| 8 | **A result cache (a question to the author, §4):** a test's result keyed by "the hash of the set + the engine version + the driver" could be reused across branches and in CI |

## 3. Example output

```text
$ reasonr test --affected --base origin/main -v
changed: Urcar.Ordering.Order.LeaveTip, Urcar.Ordering.Order.Card, screen Urcar.Ordering.MyOrder
running: 3 tests of 33 (base), 3 of 34 (acme)
  The customer leaves the driver a tip  — Order.LeaveTip
  A tip is not accepted before payment  — Order.LeaveTip
  A ride on fakes: matching, …          — Order.Card
skipped: 30 (base), 31 (acme)
```

## 4. Questions to the author

| Question | Proposal |
|---|---|
| Cache test results by hash (like Bazel) or only select tests? | Only selection at first; the cache — stage P4 after the workers (item 10) |
| How strict is the check of old tests with feature flags: an error or a warning? | An error in CI, an explicit exception with a reason |
| Analyzing changes in the platform's Go code (own adapters, primitives) | Do not analyze: a change means a full run |

## 5. Stages

| Stage | Result |
|---|---|
| **P1. Hashes and the graph** | Hashes of IR functions and schema elements, the reachability graph of tests (memory/api), `reasonr build --dump-impact` |
| **P2. Selecting tests** | Building the base version from git, `reasonr impact`, `reasonr test --affected`, `reasonrd test --affected`; the rules of a full run |
| **P3. UI and feature flags** | Places and layout expressions in the graph (`--driver web`); checking old tests when a flag is created |
| **P4. Checking the analysis and CI** | A mutator test: every function of taxi and bank gets a change in turn, and the analysis must select every test that the change makes fail (compared with a full run); a CI example in the README |

The acceptance criterion:
- a mutation run on taxi and bank: not a single failing test skipped by the analysis;
- a change of one taxi command runs fewer than 20 % of the tests;
- a commit that creates a flag and changes an old test is rejected by `reasonr impact`.

## 6. Progress

| Stage | Status | What was done |
|---|---|---|
| P1. Hashes and the graph | Done | `compiler/impact`: the nodes are IR functions (a hash of their record without positions), schema types (a canonical record without numbers), the reaction tables, the node `ui` — the UI program; test reachability by the symbols of instructions; `reasonr build --dump-impact` |
| P2. Selecting tests | Done | `compiler.Impact`: the base is `git merge-base HEAD <ref>`, extracted with `git archive` into a temporary directory (no checkout); `reasonr impact [--base] [--format json] [--allow-test-changes]`; `reasonrd test --affected [--base]` and `--impact <report.json>`; `reasonr test --affected` in a project with an image computes the selection on the host and passes it to the engine in a file (the image has no git). A full run: any project file outside `*.rr`, `api/` and `*.md` changed or added, a new extension, the base does not build |
| P3. UI and feature flags | Done (simplified) | The interface is one node: a change of the UI program on `--driver web` affects every test (instead of the step's places and layout expressions one by one). The flag check: a change creates a flag and changes the function of an old test that does not turn the flag on — an error of `reasonr impact` (exit code 1); removing a flag allows editing old tests |
| P4. Checking the analysis | Done | `engine/testrun/impact_test.go`: each of 268 functions of taxi (base and acme) and bank is broken in turn (on entry — a loop up to the iteration limit), 254 mutations make tests fail, and not a single failing test is outside the analysis's selection. `compiler/impact_test.go`: a git repository with bank — a comment affects no tests, an edit of `SetOverdraft` — 5 of 10, `reasonr.yaml` and a new `engine.yaml` — a full run, a flag with an edit of an old test — a violation, `--allow-test-changes` lifts it. Precision on taxi: a change of one command — a median of 17 % of the tests (28 of 54 commands — under 20 %) |

**Deviations from the plan.**
- Timers and fakes narrow the graph: a timer's command is reachable only if the test moves time (`time passes`), a request implementation in a fake only if the test calls the fake's commands. Without this the median was 24 %; the mutation run confirms that nothing is missed.
- The compiler and engine version is not part of the key: in the monorepo changing them is Go code outside the model project, which the project's `git diff` does not see. The nightly full run on main covers this.
- The result cache (decision 8) was not done — per the answer in §4: selection only at first.
- A separate CI example in the README was not added: the commands are in the README and RFC §27.4.
