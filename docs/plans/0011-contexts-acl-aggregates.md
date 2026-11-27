# Plan: bounded contexts, anti-corruption layer and aggregates

| | |
|---|---|
| **Basis** | [Roadmap](../roadmap.md), item 3; DDD (Evans): Bounded Context, Anti-Corruption Layer, Aggregate |
| **Status** | Accepted 2026-11-12: the decisions of §3 and the proposals in the questions of §4. B1–B5 are done, B6 — partly |
| **Branch** | `contexts-acl` |

---

## 1. Understanding

**A bounded context is where a model lives.** Inside a bounded context each name means one thing: `Order`, its `Id`, the statuses, money. Across the boundary is another model, even if an entity has the same name. So **an identifier works only inside its own bounded context**.

**An anti-corruption layer (ACL) translates one model into another.** It knows both models. The ACL is the only place where the names of two bounded contexts meet.

**An aggregate is a consistency boundary.** Inside are the root and the inner entities. Inner entities can be referenced only inside the aggregate. An aggregate references only other aggregates. Public commands and queries belong to an aggregate.

**The goal is to move a bounded context into a separate service without rewriting the model.**

## 2. What contradicts this today

| Today (RFC §23.1, the modularity design of RFC §23) | Why it gets in the way |
|---|---|
| `entity Order` in `Ordering` and `Dispatch` is one entity `Urcar.Order` with one `Id` (E1408–E1410) | The bounded contexts are tied by a shared identifier, not by translation |
| `Dispatch.Order` reacts `on Ordering.Order.placed` "in the model of the same entity" (E1411) | The reaction is possible only because of the shared `Id` |
| `Dispatch` stores `Ordering.GeoPoint`, `Ordering.Money`, `Ordering.OrderStatus`; `Ordering` shows `Dispatch.DriverInfo` | A change in one bounded context breaks another |
| A project extends the entity model of a module: `Auth.Profile` is fields of the same `Auth.User` | The same shared identifier between a module and a project |
| Two-level names "system.context": `Urcar.Ordering`, system roles `Urcar.admin` | The system is a needless level: the bounded context sets the model boundary |
| There are no aggregates and inner entities | "Route stops" with their own identity and commands cannot be expressed |

## 3. Decisions and options

### 3.1. Where the ACL lives

In the **receiving bounded context**, as in Evans: the ACL protects its own model. A `*.acl.rr` file in the bounded context directory, the declaration `acl <SourceContext>`. From the source the ACL sees what is published: public events, public commands, types. From its own bounded context — everything. When the bounded context moves into a service, its ACL goes with it.

### 3.2. How identifiers are translated — the taxi example

The task: an order is created in `Ordering` — a trip must appear in `Dispatch`. A driver is assigned in `Dispatch` — `Ordering` must find its order. `Order.Id` and `Trip.Id` are different types of different bounded contexts. The ACL must be able to get one from the other.

**Option A — the same uuid, another type.** It fits when there is exactly one trip per order.

```rr
# contexts/Dispatch/ordering.acl.rr
acl Ordering
  on Ordering.Order.placed
    [Trip.Id(id)].Open(...)              # trip 0193…a1 for order 0193…a1

# contexts/Ordering/dispatch.acl.rr
acl Dispatch
  on Dispatch.Trip.driverAssigned
    [Order.Id(id)]?.assignDriver(...)    # order 0193…a1 for trip 0193…a1
```

The value is the same, but `Trip.Id(…)` from an `Order.Id` can be written **only in an ACL**. The `Dispatch` model does not mention `Order` at all.

**Option B — a reference in a field.** It is needed when the mapping is not 1:1 (several trips per order: a repeated match) or the identifiers are of different kinds (a trip has `id sequence`). The trip stores the order number as an ordinary value of its model. Its events carry it back.

```rr
# contexts/Dispatch/trip.rr
aggregate Trip
  id sequence
  orderRef string                        # for Dispatch just an external number
  index byOrderRef(orderRef)

  event driverAssigned(orderRef string, driverName string, plate string)

# contexts/Dispatch/ordering.acl.rr
acl Ordering
  on Ordering.Order.placed
    [new Trip.Id].Open(orderRef: string(id), ...)

# contexts/Ordering/dispatch.acl.rr
acl Dispatch
  on Dispatch.Trip.driverAssigned
    [Order.Id(orderRef)]?.assignDriver(driverName: driverName, plate: plate)
```

The proposal: **A by default, B when not 1:1**. A third option — a mapping table kept by the engine — is not needed: a new store for what A and B already solve.

### 3.3. An ACL reaction: in the same unit of work or a separate one

| Option | How |
|---|---|
| **1. In the same unit of work while the bounded contexts share a process** | As today: `Ordering.Cancel` and the `Dispatch` reaction commit together or not at all |
| **2. Always a separate unit of work** | The `Ordering` event commits. The `Dispatch` reaction runs after it in its own unit of work (at-least-once delivery, in order per aggregate) |

I proposed option 2 so that **the behavior does not change when a bounded context moves into a service**: the model lives from the start in the conditions it will face after the move.

What changes with option 2 — the order cancellation example:

- **A race.** A customer cancelled an order: `Ordering.Cancel` committed. The `Dispatch` reaction "withdraw the offer" has not run yet. At that moment a driver accepts the offer, and `Dispatch` assigns them to a cancelled order. With option 1 this cannot happen: the cancellation and withdrawing the offer are one transaction.
- **A partial result.** If the `Dispatch` reaction failed, the order is already cancelled, and the driver stays busy. With option 1 a failed reaction undoes the cancellation too.

The proposal is **option 1, by your question**: while the bounded contexts share a process, an ACL reaction is in the same unit of work. Before moving a bounded context out (stage B6) an analysis is needed: the model must withstand the race and partial results. The extraction tool lists the ACL reactions of the extracted bounded context that today run in one transaction with the source.

### 3.4. The transaction boundary inside a bounded context — decided: several aggregates

`Driver.Accept` atomically takes the driver and assigns the trip.

### 3.5. Syntax — decided: `aggregate`

`aggregate Order` at the top level. There is no top-level `entity` anymore. Inner entities are `entity`, each in its own file, so that the aggregate file does not grow:

```rr
# contexts/Ordering/order.rr
aggregate Order
  id uuid
  status OrderStatus

  ## The driver reached a stop
  command(driver) ArriveAt(stop Stop.Id)       # a public command of the aggregate
    [stop].arrive()

# contexts/Ordering/order.stop.rr
## A route stop: its own identity inside the order
entity Order.Stop
  id int                                       # unique inside the order
  point GeoPoint
  arrivedAt timestamp?

  command arrive()                             # private commands only
    arrived()
  event arrived()
    arrivedAt = now
```

- Inside the aggregate its entities are listed as a source: `for s in [Stop]`.
- They are stored in the aggregate document.
- An inner entity has no public commands and queries.

### 3.6. No systems — only bounded contexts (decided)

- Directories: `contexts/Ordering/`, `contexts/Dispatch/`.
- Full names: `Ordering.Order`, `Ordering.Money`.
- API: `/api/Ordering.Order/{id}/Cancel`.
- A module brings its own bounded contexts: `Auth` instead of `Auth.Accounts`.
- Bounded context names are unique in a project.
- Roles are shared by the project (`admin`): authentication grants them, not a bounded context.

### 3.7. Inner entity identifiers in value objects

By DDD a value object **may** hold the identifier of another aggregate: `OrderLine(productId, qty)`. It may hold the identifier of an inner entity only while it stays inside that entity's aggregate: in the aggregate's fields, its events, the parameters and results of its commands and queries. There the root identifier is always next to it. Storing such a value in another aggregate is an error: from outside an inner entity is not addressable.

### 3.8. Creating an aggregate

Today creation is implicit (RFC §10.3): the caller chooses the identifier in advance (`new Order.Id` in the UI and tests, a uuid in the API path), and the first command on a non-existent id creates the entity. The strategies already exist: `id uuid`, `id sequence` (incremental, issued by the engine), `id string` (a natural key), composite.

Missing is creation when **the engine issues the identifier** (`sequence`): an API client does not know the number in advance. The proposal is a **creation command**, the analogue of a static factory method:

```rr
aggregate Invoice
  id sequence

  command(accountant) create Issue(amount Money)     # POST /api/Billing.Invoice/Issue — without an id
    issued(amount: amount)
```

The engine issues an identifier by the strategy, runs the command on the new aggregate and returns the id in the response: `{"status":"accepted","id":"42",…}`. If the aggregate already exists, the error `alreadyExists` arises by itself, without `if [id]`. Commands without `create` still create an aggregate implicitly — `uuid` and `string` need that.

## 4. Questions to the author

| Question | Proposal |
|---|---|
| Translating identifiers — §3.2 | A by default, B when not 1:1 |
| An ACL reaction — §3.3 | In the same unit of work while the bounded contexts share a process |
| Does an ACL see only what the source publishes? | Yes: otherwise the source cannot move into a service |
| May an ACL call the source's public commands? | Yes. Reading the source's queries synchronously — no: the receiver keeps the data it needs from events |
| Shared types (money, a map point) | Each bounded context declares its own. A shared kernel is a module with types, if copies multiply |
| How does an `Auth` user become a taxi customer if foreign identifiers are not visible? | Authentication gives the caller `Auth.User`. The `Ordering` ACL for `Auth` maps it to its own actor `Customer` by a unique field |
| May an aggregate be declared in several files (like a partial class)? | Yes, within a bounded context: the commands of a large aggregate across files |
| The creation command `create` — §3.8 | Yes |
| Translation to external systems with the same `acl` (taken from the API governance design, RFC §31.3.1) | Yes, stage B6 |

## 5. Stages

| Stage | Result |
|---|---|
| **B1. Only bounded contexts** | One-level names, `contexts/<Context>/` directories, project roles; the `auth` module is the bounded context `Auth`; migrating the examples |
| **B2. The bounded context owns the model** | An aggregate belongs to a bounded context. Identifiers and types of another bounded context outside an ACL are an error. E1408–E1411 go away |
| **B3. ACL** | `acl`, identifier conversion, reactions in the same unit of work. The taxi: `Dispatch.Trip`, its own types, two ACLs; `Auth` → `Customer` through an ACL |
| **B4. Aggregates** | `aggregate`, `entity Aggregate.Entity` in their own files, `[Stop]`, identifier visibility (§3.7), storage in the aggregate document, `create` |
| **B5. Docs** | RFC §5, §10, §23, §25, AGENTS.md, README; `reasonr graph` — the context map with ACLs |
| **B6. Extracting a bounded context** | An engine process serves some bounded contexts (`contexts:` in `engine.yaml`), ACLs between processes go through the bus, the reaction analysis (§3.3); translation to external systems through `acl` |

Done when:
- in the taxi `Ordering` and `Dispatch` mention each other only in `*.acl.rr`;
- the taxi and bank tests pass on `memory`, `api`, `web`;
- with B6 `Dispatch` works as a separate process, and the same tests pass.

## 6. Progress

| Stage | Status | What is done |
|---|---|---|
| B1. Only bounded contexts | Done | `contexts/<Context>/` directories (E1402), full names `Ordering.Order`, project roles, a feature flag belongs to a bounded context (`Ordering.tips`); the `auth` module is the bounded context `Auth`; the taxi — `Ordering` and `Dispatch`, the bank — `Accounts`. E1405 is removed |
| B2. The bounded context owns the model | Done | Same-named entities of bounded contexts are different; entity models (a shared `Id`, the `entity` schema, method lookup across models) are removed. A declaration of another bounded context outside ACLs, tests and apps is E1407. E1408–E1411 are removed |
| B3. ACL | Done | `*.acl.rr` (E1412, E1413), `acl Source` (E1414): `on Source.Entity.event` (E1415, E1416) — in the same unit of work after the root command, in event order; `actor Source.Actor as Actor` (E1417); conversions `Trip.Id(id)`, enumerations by name `CarClass(x)` (E1418). The taxi: `Dispatch.Trip` with its own types, ACLs both ways, `Auth.User` is a customer through an ACL. RFC §23, AGENTS.md, README |
| B4. Aggregates | Done | `aggregate` (a top-level `entity` is E1419 with a fix), `entity Aggregate.Entity` (E1420–E1423): `[stop]`, `[Stop]`, `new Stop.Id` (sequence — a number inside the aggregate), storage in a map field of the aggregate, events "Trip.Offer.closed(id: …)", folding; partial aggregates; `create` (E1424, `alreadyExists`, `POST /api/{Type}/{Command}` with `id` in the response, OpenAPI, appapi). The taxi: `Trip.Offer`; the bank: `Account.Open` |
| B5. Docs | Done | RFC §5, §10 (§10.7, §10.8), §14, §25, the grammar; AGENTS.md, README; `reasonr graph` — ACL reactions, `--contexts` — the context map |
| B6. Extracting a bounded context | Partly done | `contexts:` in `engine.yaml`: a process serves its own bounded contexts (foreign commands and queries — 421), the ACL reactions of foreign bounded contexts are not part of its units of work; the receiving process runs them on an event from the effect stream in its own unit of work with a retry key (a unit remembers the bounded contexts of the process that ran it); a reaction error is a dead letter. `reasonr extract <Context>` is the analysis of §3.3. The test: two processes on one PostgreSQL (`Ordering`, `Auth` and `Dispatch`), order → trip → offer → order. Not done: translation to external systems through `acl`; running the acceptance tests on split processes |

**Deviations of B1–B3** (for the author's decision):

1. **An `Auth` user is a customer with the same identifier** (`actor Auth.User as Customer`, option A of §3.2), not by a unique field as in the answer to the §4 question. The mapping is 1:1, no field and index are needed, and the engine does not look up a record on each call. The by-field option (`actor Auth.User as Customer by account`) can be added when a non-1:1 mapping is needed.
2. **Enumerations are converted by name** — `CarClass(requestedClass)` in an ACL (E1418 if its own enumeration lacks the value). Without it each enumeration would be converted with a chain of `if`.
3. **Tests convert identifiers**: `[Trip.Id(o1)].DriverFound(…)`, like an ACL — a test calls the whole project.
4. **The "whole system" rollout level is called "all"** (`all`); a feature flag belongs to a bounded context.
5. **The taxi:** an order has no driver field — `Dispatch` sends the driver's notifications (`rideCancelled`, `tipReceived`) on order events; the home address moved from `Auth.Profile` to the customer (`Customer.SetHome`, `Customer.Home`); the end-to-end tests are `Ordering/ride.test.rr`; the matching request carries `tripId`, and the integration service answers with `/api/Trip/{id}/DriverFound`.
6. **Table and collection names** by full name changed (`ordering__order` instead of `urcar__ordering__order`): the new program does not read the data of models with systems — the example database (`reasonr dev`) must be recreated.

**Deviations of B6:**

7. **The transport between processes is the shared main store** (the outbox effect stream), not the bus: the processes share PostgreSQL or MongoDB, and each receiver has its own dispatcher cursor. Separate stores and delivery through Kafka are the next step.
8. **The acceptance tests are not run on split processes:** reactions across the boundary are asynchronous, and a test would have to wait for delivery after each step. The check is an integration test of two processes (`TestSplitContexts`).
9. **Translation to external systems through `acl`** (from the API governance design) is not done: external systems are still declared `external` in a bounded context.
10. **A race found by the two-process test:** the matching answer arrived before the ACL opened the trip and "created" it. The taxi: `DriverFound` and `NoDriverFound` on an unopened trip do nothing (`if not [id] …`) — so the model withstands an asynchronous ACL (§3.3).
11. Two processes starting at the same time on an empty database may hit a deadlock while creating the system tables (PostgreSQL): the second process starts after the first. This behavior existed before.
