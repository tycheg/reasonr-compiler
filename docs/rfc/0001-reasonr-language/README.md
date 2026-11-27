# RFC-0001: ReasonR — a business logic language

| | |
|---|---|
| **Status** | Draft, revision 8 |
| **Author** | Nikita Matsko |
| **Date** | 2026-10-02 |
| **Working name** | ReasonR, files `*.rr` |
| **Inspired by** | DDD, CQRS, Event Sourcing |
| **Implementation plan** | [Roadmap](../../roadmap.md) |

The RFC is split into chapter files; the section numbers run through all of them. The `rr` blocks whose first line is a path (`# taxi/contexts/Ordering/order.rr`) are the example models in `examples/`: `go test . -update` in `docs/` writes them.

## Contents

**[Introduction](01-introduction.md)**

- [1. Summary](01-introduction.md#1-summary)
- [2. Motivation](01-introduction.md#2-motivation)
- [3. Goals and non-goals](01-introduction.md#3-goals-and-non-goals)
- [4. Design principles](01-introduction.md#4-design-principles)
- [5. Model overview](01-introduction.md#5-model-overview)

**[The language](02-language.md)**

- [6. Lexis and syntax](02-language.md#6-lexis-and-syntax)
- [7. Type system](02-language.md#7-type-system)
- [8. Expressions and built-in values](02-language.md#8-expressions-and-built-in-values)
- [9. Enumerations and value objects](02-language.md#9-enumerations-and-value-objects)
- [10. Aggregates and entities](02-language.md#10-aggregates-and-entities)
- [11. Commands](02-language.md#11-commands)
- [12. Events](02-language.md#12-events)
- [13. Notifications](02-language.md#13-notifications)
- [14. Errors](02-language.md#14-errors)
- [15. Timers](02-language.md#15-timers)
- [16. Projections](02-language.md#16-projections)
- [17. Queries](02-language.md#17-queries)

**[External systems, access, personal data, extensions, UI](03-systems-access-ui.md)**

- [18. External systems](03-systems-access-ui.md#18-external-systems)
- [19. Access: roles and actors](03-systems-access-ui.md#19-access-roles-and-actors)
- [20. Personal data and crypto-shredding](03-systems-access-ui.md#20-personal-data-and-crypto-shredding)
- [21. Multitenancy and extensions](03-systems-access-ui.md#21-multitenancy-and-extensions)
- [22. Interface (UI)](03-systems-access-ui.md#22-interface-ui)

**[Project and execution](04-project.md)**

- [23. Project: parts and directories](04-project.md#23-project-parts-and-directories)
- [24. Execution semantics](04-project.md#24-execution-semantics)

**[Example: taxi, Ordering](05-example-taxi.md)**

- [25. Example 1: taxi](05-example-taxi.md#25-example-1-taxi)

**[Example: taxi, Dispatch and screens](06-example-taxi-dispatch.md)**

- [25.4. Driver and matching: bounded context Dispatch](06-example-taxi-dispatch.md#254-driver-and-matching-bounded-context-dispatch)
- [25.5. Projection, external systems, screens](06-example-taxi-dispatch.md#255-projection-external-systems-screens)
- [25.6. What to notice](06-example-taxi-dispatch.md#256-what-to-notice)

**[Example: bank](07-example-bank.md)**

- [26. Example 2: a bank transfer](07-example-bank.md#26-example-2-a-bank-transfer)

**[Acceptance tests](08-tests.md)**

- [27. Acceptance tests](08-tests.md#27-acceptance-tests)

**[Compiler, runtime, engine abstraction](09-compiler-engine.md)**

- [28. Compiler](09-compiler-engine.md#28-compiler)
- [29. Go runtime](09-compiler-engine.md#29-go-runtime)
- [30. Engine abstraction](09-compiler-engine.md#30-engine-abstraction)

**[The default engine, tools, decisions](10-default-engine.md)**

- [31. The default engine implementation](10-default-engine.md#31-the-default-engine-implementation)
- [32. Schema and migrations](10-default-engine.md#32-schema-and-migrations)
- [33. Tools](10-default-engine.md#33-tools)
- [34. Safety guarantees for "vibe coding"](10-default-engine.md#34-safety-guarantees-for-vibe-coding)
- [35. Alternatives considered](10-default-engine.md#35-alternatives-considered)
- [36. Risks](10-default-engine.md#36-risks)
- [37. Decisions and open questions](10-default-engine.md#37-decisions-and-open-questions)
- [39. Glossary](10-default-engine.md#39-glossary)
