## 6. Lexis and syntax

### 6.1. Rules

- Blocks are set by indentation. A new block opens with an indent of **exactly 2 spaces** more than its parent; it may close at any of the open levels. Tabs are forbidden.
- **Declarations have no colons.** A declaration starts with a keyword (`entity`, `command`, `event`, `query`…) or a field name, and the body follows below, indented.
- **A colon is used only to pass arguments:** `driverAssigned(driverId: driverId)`. The only exception is a map literal `["a": 1]`.
- A field or parameter is declared as `name Type [attributes]`, for example `driver Driver.Id?` or `(phoneNumber string personal)`.
- One statement per line. A long expression may wrap inside parentheses.
- `#` starts a comment to the end of the line. `##` is a doc comment for the next declaration; it goes to the documentation, `explain` and LSP hints. Interface texts — §22.11.

Why not real YAML: in YAML, expressions, conditions and loops become strings without types and without clear errors. ReasonR takes the visual structure of YAML, but its grammar is its own and strict (see [§28.2](09-compiler-engine.md#282-grammar-ebnf-abridged)).

### 6.2. Naming conventions

| What | Style | Example |
|---|---|---|
| Entities, actors, projections, value objects, enumerations, external systems, apps, screens, primitives | `PascalCase` | `Order`, `OrderMart`, `OrderCard` |
| **Public** commands and queries | `PascalCase` | `Cancel`, `MyOrders` |
| **Private** commands and queries | `camelCase` | `assignDriver`, `info` |
| Fields, parameters, events, notifications, errors, indexes, rules, roles, variables, enumeration values | `camelCase` | `driverAssigned`, `admin` |

The visibility of a command or query is set by the case of its first letter; there are no `public` and `private` modifiers. The compiler checks the naming style; a violation is an error.

### 6.3. Literals

| Type | Examples |
|---|---|
| Integers | `0`, `42`, `1_000_000` |
| Fractions (`decimal` or `float`, by context) | `10.50`, `0.3` |
| Strings | `"text"`, interpolation `"Order {orderId}"`; literal braces — `{{` and `}}`; escapes `\" \\ \n \t \u{…}` |
| Booleans | `true`, `false` |
| No value | `none` |
| Duration | `500ms`, `30s`, `5m`, `2h`, `1d` |
| Date and time | `date"2026-10-02"`, `timestamp"2026-10-02T12:00:00Z"` |
| List or set (by context) | `[a, b, c]`, `[]` |
| Map | `["a": 1, "b": 2]`, `[:]` |

### 6.4. Keywords

**Reserved** — cannot be names:

```
aggregate entity actor value enum projection external role app screen primitive texts migration test
extend command event notification error query rule request index
let if else for in while break continue return delete forget
and or not is none true false new
```

**Contextual** — keywords only in their position; elsewhere they are ordinary names:

```
id unique personal check timeout was on fake feature acl create
where sort page limit select desc all
after hasErrors now today me caller schedule self
given when then as matches time offset passes exclude because to
platforms language default auto back binds part children params inputs screens field input
```

For example, `page` is a keyword in a search (`page pageReq`) and in the type `page<T>`, but may be a field name. Built-in values (`id`, `now`, `me`…) cannot be redeclared as a field, parameter or variable.

---

## 7. Type system

### 7.1. Primitives

| Type | Default | Description |
|---|---|---|
| `int` | `0` | 32-bit signed integer |
| `bigint` | `0` | 64-bit signed integer |
| `float` | `0` | 64-bit IEEE 754. Not for money; the linter warns |
| `decimal` | `0` | Fixed-point decimal: 128 bits, 18 digits after the point (up to 20 before it) |
| `string` | `""` | UTF-8 |
| `bool` | `false` | |
| `uuid` | the zero UUID | |
| `date` | `date"0001-01-01"` | Calendar date |
| `time` | `00:00` | Time of day |
| `timestamp` | `timestamp"0001-01-01T00:00:00Z"` | A moment in time (UTC) |
| `duration` | `0s` | Duration |
| `bytes` | empty | Binary data of limited size |

**`decimal` arithmetic.** The value is a 128-bit integer divided by 10¹⁸. `+` and `-` are exact; `*` and `/` round the result to 18 digits after the point, half to even (banker's rounding); `round(x, n)` rounds to `n` digits half away from zero, as in accounting: `round(2.345, 2) == 2.35`, `round(-2.345, 2) == -2.35`. Going out of range is an overflow ([§7.5](#75-rules)).

### 7.2. Composite and built-in types

| Type | Default | Description |
|---|---|---|
| `list<T>` | `[]` | Ordered list (array) |
| `set<T>` | `[]` | Set |
| `map<K, V>` | `[:]` | Map |
| `T?` | `none` | Optional type |
| `page<T>` | — | A page of query results (`items list<T>`, `next string?`) |
| `PageRequest` | `size 20` | A page request (`size int`, `cursor string?`) |
| `<Entity>.Id` | **none** | The identifier type of an entity: `Order.Id`, `Driver.Id` |
| `Timer.Id` | **none** | Timer identifier |

All collections are **immutable**. Operations return a new collection: `items.with(x)`, `items.without(x)`, `prices.put(k, v)`.

`set` and `map` keep **insertion order**: `for`, `.first`, `.last`, `.keys` and `.values` follow it. `with` of an existing element and `put` of an existing key do not move it; `without` removes the element. Equality of sets and maps does not depend on order.

### 7.3. Default values

Any field of an entity or projection without an explicit `= value` gets the default value of its type:

- an enumeration — its first declared value;
- a value object — all its fields with their defaults;
- **identifiers** (`Order.Id`, `Timer.Id`) have **no** default, so a field of such a type is always optional (`Driver.Id?`) and defaults to `none`. The language has no zero identifiers;
- if a type has no default (a value object with an identifier field or with a violated `check`), the field must be `T?` or have an explicit `= value`. The compiler checks this.

### 7.4. Identifier types

An identifier type is formed from the entity name with a dot: `Order.Id`, `Driver.Id`. It is **nominal**: `Order.Id` and `Driver.Id` are incompatible even if both hold a `uuid`. So `[orderId]` unambiguously points to an `Order` entity.

### 7.5. Rules

- **No implicit conversions** except lifting `T` → `T?`. `int + decimal` is a compile error. Conversions are explicit: `decimal(x)`, `int(x)`, `string(x)`, and between an identifier type and its base identifier: `OrderMart.Id(orderId)`, `Order.Id(martId)` (see [§16](#16-projections)).
- **No `null`.** Only `T?` can have no value, written as `none`.
- **Flow typing (smart cast).** After `if x is none` that exits with `return`, and inside an `if x is not none` branch, the variable `x` has type `T`. Narrowing works for entity fields too, because fields do not change within a command.
- **Structural equality** for value objects, collections and identifiers.
- **Overflow and division by zero** are an execution fault, not a business error. The effects are not applied; the client gets `internalError`, the developers get a trace.

---

## 8. Expressions and built-in values

### 8.1. Operators (by decreasing precedence)

| Operator | Description |
|---|---|
| `.` `?.` `()` `[ ]` | Field or method access, safe call, call, entity access |
| `-x` `not x` | Unary |
| `*` `/` `%` | Multiplicative |
| `+` `-` | Additive; for `string` — concatenation, `timestamp + duration` — a shift in time |
| `??` | Default for an optional: `name ?? "—"` |
| `==` `!=` `<` `<=` `>` `>=` `in` `not in` `is none` `is not none` | Comparisons |
| `and` | Logical AND (lazy) |
| `or` | Logical OR (lazy) |

### 8.2. Built-in values

| Name | Type | Where | Description |
|---|---|---|---|
| `id` | `<Entity>.Id` | commands, events | The identifier of the current entity |
| `now` | `timestamp` | commands, queries | The time fixed by the engine for the root command; one for the whole unit of work |
| `today` | `date` | commands, queries | The date of `now` |
| `after` | entity snapshot | commands | The state of the current entity with the events the command has already raised (see [§11.5](#115-fields-after-and-state-visibility)) |
| `hasErrors` | `bool` | commands, queries | Whether the current command, including nested calls, has errors |
| `me` | `<Actor>.Id` | methods with access `(<Actor>)` | The identifier of the calling actor |
| `caller` | `Caller` | commands, queries | `caller.roles set<string>` |
| `new T.Id` | `T.Id` | commands, screens, tests | A new identifier by the entity's strategy |

### 8.3. Standard library

Functions are **pure**; the set is fixed and grows only through a PR to the language code.

- conversions: `int()`, `bigint()`, `float()`, `decimal()`, `string()`;
- numbers: `abs`, `min`, `max`, `round(x, digits)`, `floor`, `ceil`;
- strings: `.length`, `.trim()`, `.lower()`, `.upper()`, `.startsWith()`, `.contains()`, `.matches(regex)`;
- collections: `.count`, `.isEmpty`, `.contains(x)`, `.with(x)`, `.without(x)`, `.first`, `.last`, `.at(i)`, `.get(k)`, `.put(k, v)`, `.keys`, `.values`, `.sum`; `.first`, `.last`, `.at` and `.get` return an optional value;
- time: `.date`, `.addDays(n)`, `.between(a, b)`, `duration.seconds`.

Details:

- `.matches(regex)` matches the **whole** string; `.length` is the number of Unicode characters;
- `.between(a, b)` includes both bounds; `list.without(x)` removes every element equal to `x`;
- `int(x)` and `bigint(x)` of a `float` or `decimal` drop the fraction; a failed conversion (`int("x")`, out of range) is an execution fault;
- `round(x, n)` of an integer with `n < 0` rounds to tens, hundreds… away from zero; for `float`, an infinite or NaN result and division by zero are an execution fault;
- `string(x)` and interpolation print an enumeration by the value name, an identifier by its content, a duration in the largest exact unit (`90s`, `2h`), a value object as `Type(field: value, …)`;
- an argument of a public command or query that the client omits gets the parameter's default; without one — the error `invalidArgument(path, "required")`. `PageRequest` defaults to `size 20`; the page size is 1 to 100.

---

## 9. Enumerations and value objects

### 9.1. Enumerations

```rr
## Order status
enum OrderStatus
  searching     ## looking for a driver (the default)
  offered       ## the offer is sent to a driver
  assigned      ## the driver is on the way
  completed     ## the ride is over, waiting for payment
  paid
  unpaid
  cancelled
  noDrivers
```

The default is the first value. When the type is clear from the context, a value is written without a prefix (`status = searching`), otherwise in full: `OrderStatus.searching`.

If a value's short name matches a field, parameter or variable name, they take precedence. If an enumeration with that value is also expected there, the compiler reports an ambiguity and asks for the full name: `DriverStatus.free`.

### 9.2. Value objects

1. Immutable.
2. All fields are public.
3. May have methods that **return** primitives or value objects. Methods are pure: `let`, `if`, `for`, `return` are available, but no effects.
4. May have `check` invariants. If an API argument violates an invariant, the client gets `invalidArgument`. If an invariant is violated inside logic, it is an execution fault.

```rr
enum Currency
  rub
  usd

value Money
  amount decimal
  currency Currency

  check amount >= 0

  times(k decimal) Money
    return Money(amount: round(amount * k, 2), currency: currency)

value GeoPoint
  lat float
  lon float

  check lat >= -90 and lat <= 90
  check lon >= -180 and lon <= 180
```

A method is declared as `name(parameters) ResultType`, a field as `name Type`. A value object is created by a call with named arguments: `Money(amount: 100, currency: rub)`.

---

## 10. Aggregates and entities

### 10.1. Structure

```rr
aggregate <Name>
  id <strategy or type>

  <field> <Type> [= value] [personal[(<subject>)]]
  ...

  index <name>(<field> [desc], ...) [unique <error>]

  command[(<access>)] [create] <name>(<parameters>)
  event <name>(<parameters>)
  notification <name>(<parameters>)
  error <name>[(<parameters>)]
  query[(<access>)] <name>(<parameters>) <ResultType>
  rule <name>(<parameters>) <ResultType>
```

An **aggregate** is a model of a real object in its bounded context: an order, a driver, an account, a shift. It is also a consistency boundary: public commands and queries belong to the aggregate; inside are the root and inner entities (§10.7). An aggregate refers to other aggregates by identifier. There is no top-level `entity` (E1419): `entity` is an inner entity of an aggregate. An actor (`actor`, §19) is an aggregate that is a user of the system. Below, "entity" means an aggregate, an actor or an inner entity unless stated otherwise.

### 10.2. Identifier and uniqueness

| Declaration | Identifier type | Who sets it |
|---|---|---|
| `id uuid` | `Order.Id` over `uuid` (v7) | The client or `new Order.Id` |
| `id sequence` | `Invoice.Id` over `bigint`, incremental | The engine: `new Invoice.Id` or a create command (§10.3) |
| `id string` | `Customer.Id` over `string` | The caller (a natural key) |
| `id ShiftKey` | Composite, a value object | The caller |
| `id Order.Id` (projections only) | `OrderMart.Id` based on `Order.Id` | The conversion `OrderMart.Id(orderId)` |

```rr
value ShiftKey
  driverId Driver.Id
  day date

aggregate Shift
  id ShiftKey            # composite identifier, type Shift.Id
  startedAt timestamp?
```

**Uniqueness** is declared on an index: `unique` and the name of the error the client gets on a violation.

```rr
actor Customer
  id uuid
  phone string? personal

  index byPhone(phone) unique phoneTaken

  error phoneTaken
```

- Uniqueness is checked against the state **after** the unit of work. If two entities get the same value, the unit of work is rejected with `phoneTaken` and its effects are not applied.
- `none` values do not take part in uniqueness.
- Composite uniqueness: `index byOwnerName(owner, name) unique nameTaken`.
- The error may have parameters named after the index fields (the last path segment); the engine fills them with the values.
- Unique indexes may be declared on projections as well.
- How to enforce uniqueness (a unique database index, a lock, a separate table) is up to the engine.

### 10.3. Existence and entity access

**An entity exists if at least one event has happened to it.** An identifier always refers to an entity, but an entity to which nothing has happened does not exist, and its fields have default values. So an aggregate whose identifier the caller chooses (`uuid`, `string`, composite) is created by its first command.

A **create command** is for an aggregate whose identifier the engine issues (`id uuid` or `id sequence`): `command(access) create Name(…)` (E1424). The client calls it without an identifier — `POST /api/Billing.Invoice/Issue`; the engine issues an identifier by the strategy, runs the command on the new aggregate and returns the id in the response: `{"status":"accepted","id":"42",…}`. If an aggregate with the issued id already exists, the `alreadyExists` error arises by itself. In tests, the test chooses the identifier itself, as with `new`: `[a1].Open() as c1`; through the test mode API — `POST /api/{Type}/{id}/{Command}`. In the UI, a create command form has no target.

```rr
aggregate Invoice
  id sequence

  command(accountant) create Issue(amount Money)     # POST /api/Billing.Invoice/Issue — no id
    issued(amount: amount)
```

| Form | Where | Meaning |
|---|---|---|
| `[x]` | commands | An opaque reference `Ref<T>?`: `none` if the entity does not exist or `x` is `none`. It can check existence and call a command, but not read fields |
| `[x]` | queries of its own type | `T?`: the record with fields |
| `if [x]` / `if not [x]` | everywhere | Short for `if [x] is not none` / `if [x] is none` |
| `[x].cmd(...)` | commands | A command call. If the entity does not exist, the command starts with default fields — **that is how entities are created**. `x` must not be optional |
| `[x]?.cmd(...)` | commands | A call **only if the entity exists**. If it does not or `x` is `none`, nothing happens and there is no error |
| `[Order]` | queries of its own type | All entities of the type — a search source |
| `[stop]`, `[Stop]` | commands and rules of the aggregate | The inner entity record `Stop?` and all its records `list<Stop>` (§10.7) |
| `[martId]` | projection queries | A projection row: `OrderMart?` (`martId` has type `OrderMart.Id`) |
| `[OrderMart]` | projection queries | All projection rows — a search source |
| `delete [martId]` | commands | Delete a projection row |

The short form `if [x]` is allowed **only** for entity and projection access. Other optional values need an explicit `is none` / `is not none`, so that `if balance` does not become a trap.

**Access or list.** A one-element `[a]` may be either entity access or a list literal. The type decides:

1. `[Order]` (a type name) — a search.
2. If a collection (`list`, `set`) is expected there — a literal: `exclude: [d1]`, `status in [searching]`.
3. Otherwise, if the element is an entity or projection identifier (`Order.Id`, `OrderMart.Id`, optional included) — access: `if [id]`, `[driver]?.releaseOrder(...)`.
4. In every other case — a list literal.

```rr
command(self) SignUp(phoneNumber string personal)
  if [id]                          # the entity already exists
    alreadyRegistered()
    return
  signedUp(phoneNumber: phoneNumber)

command(customer, support) Cancel(reason string)
  ...
  [candidate]?.withdrawOffer(orderId: id)    # candidate is Driver.Id?; if none, nothing happens
  [driver]?.releaseOrder(orderId: id)
```

`[id]` inside the entity's own command tells whether the entity existed **when the command started**.

### 10.4. Fields

- Fields are **private**. Only commands, events, queries and rules of the same entity read them.
- Fields change **only in events**.
- A default is set explicitly (`blocked bool = false`) or comes from the type ([§7.3](#73-default-values)).
- Personal data is marked with the `personal` attribute (see [§20](03-systems-access-ui.md#20-personal-data-and-crypto-shredding)).

### 10.5. Indexes

Indexes are declared in entities and projections. Queries search only by them ([§17](#17-queries)). An index field may be a path into a value object.

```rr
index byCustomer(customer, placedAt desc)
index byStatus(status, car.carClass)
index byPlate(car.plate) unique plateTaken
```

### 10.6. Methods

| Method | Who calls it | Purpose |
|---|---|---|
| `command` | API, UI, other commands, timers | Business logic. Returns nothing |
| `event` | Only commands of this entity | Changes fields. Returns nothing |
| `notification` | Only commands of this entity | A message to the client |
| `error` | Only commands and queries of this entity | An error message |
| `query` | API, UI, other queries | Reading; returns a value object |
| `rule` | Commands and queries of this entity | A pure function an extension may override ([§21](03-systems-access-ui.md#21-multitenancy-and-extensions)) |

### 10.7. Inner entities

An **inner entity** is a part of an aggregate with its own identity inside it: a route stop, an invoice line, an offer to a driver. It is declared with the aggregate's name, usually in its own file, so the aggregate file does not grow too large:

```rr
# taxi/contexts/Dispatch/trip.offer.rr — in full in §25.4
entity Trip.Offer
  id sequence                      # offer number within the trip: 1, 2, 3…
  driver Driver.Id?
  outcome OfferOutcome

  command close(result OfferOutcome)
    closed(result: result)

  event closed(result OfferOutcome)
    outcome = result
```

- The aggregate is an `aggregate` or `actor` of its own bounded context (E1420). Inside the aggregate, the inner entity's short name refers to it: `Offer`, `Offer.Id`; outside — `Trip.Offer`.
- The identifier is unique within the aggregate: `uuid`, `string`, a value object or `sequence` — the number following the largest in the aggregate (`new Offer.Id`).
- It has fields, private commands, events, errors and rules. No public commands or queries, no indexes (E1421): the client calls the aggregate, and the aggregate calls its inner entities.
- It is addressed only inside its aggregate (E1422): commands, rules and reactions of the aggregate and its inner entities read `[offer]` (an `Offer?` record), enumerate `for o in [Offer]` and call `[offer].close(…)`, `[new Offer.Id].open(…)`. Queries do not read it: a query has no aggregate instance.
- Only the aggregate and its inner entities store and pass its identifier — fields, events, parameters and results (E1423); a value object may contain it, but such a value in another aggregate is an error: an inner entity is not addressable from outside.
- **It is stored in the aggregate document**: the aggregate field `Offer` is a map id → state. Its events are events of the aggregate, `Trip.Offer.closed(id: 1, result: accepted)`, with its id as the first argument: projections, the bus and state folding see them. A unit of work commits the aggregate together with its inner entities.

### 10.8. An aggregate in several files

A large aggregate may be declared in parts across several files of its bounded context, like a partial class: `aggregate Order` in `order.rr` and `order.admin.rr` is one aggregate, members in file order. The `id` line is in one of the parts; a repeated member is an error, as in a single declaration.

---

## 11. Commands

### 11.1. Public and private

| Name | Visibility | Who may call it |
|---|---|---|
| Capitalized: `Cancel` | Public | API, UI, other commands, timers |
| Lowercase: `assignDriver` | Private | Only other commands (of its own or another entity) and timers |

Access to a public command is given in parentheses ([§19](03-systems-access-ui.md#19-access-roles-and-actors)):

```rr
command(admin) Register(driverName string, driverCar Car)        # role admin
command(self) GoOnline()                                         # the actor itself
command(customer, support) Cancel(reason string)                 # the order's customer or role support
command Ping()                                                   # any authenticated caller
command offerOrder(orderId Order.Id, pickup GeoPoint)             # private
```

### 11.2. What a command can do

| # | Capability | Syntax |
|---|---|---|
| 1 | Call a command of its own entity | `searchAgain(skipped: driverId)` |
| 2 | Access any entity by identifier and call its command | `[driverId].offerOrder(...)`, `[driver]?.releaseOrder(...)` |
| 3 | Check that an entity exists | `if [orderId]`, `if not [target]` |
| 4 | Create an entity — call a command on a new id | `[new Order.Id].place(...)` |
| 5 | Raise an event of the current entity | `driverAssigned(driverId: driverId)` |
| 6 | Send a notification | `orderPlaced(orderId: id)`, addressed: `... to customer` |
| 7 | Raise an error | `offerNoLongerValid()` |
| 8 | Write a whole projection row (upsert) | `OrderMart(id: OrderMart.Id(id), status: status, placedAt: placedAt)` |
| 9 | Delete a projection row | `delete [martId]` |
| 10 | Schedule a timer | `let t = schedule(at: now + 20s, run: expireOffer(driverId: d))` |
| 11 | Delete a timer | `delete offerTimer` |
| 12 | Send a request to an external system | `Matching.findDriver(orderId: id, ...)` |
| 13 | Crypto-shred a subject's data | `forget id` |
| 14 | Declare a local variable | `let total = price.times(2)`, `let perKm decimal = 30` |
| 15 | Assign a local variable | `total = total.times(0.9)` |
| 16 | Read (but not change) arguments and fields | `if status == offered` |
| 17 | Call a rule | `perKmRate(requestedClass: requestedClass)` |
| 18 | Branches and loops | `if` / `else if` / `else`, `for x in xs`, `while`, `break`, `continue`, `return` |
| 19 | Check for errors | `if hasErrors` |
| 20 | See the state after events | `after.balance` |

What a command **cannot** do: change fields directly, read fields of other entities, call queries, wait for an external system's answer, return a value.

### 11.3. Calls and the naming rule

- A call to another entity uses a dot and named arguments: `[driverId].appointOrder(orderId: orderId)`. A command of its own entity is called without a prefix: `appointOrder(orderId: orderId)`.
- Names of **fields**, **parameters** and **local variables** must not coincide. Then `driver = driverId` reads unambiguously: the field on the left, the argument on the right.
- As a statement, `Type(...)` means only a projection upsert. A value object constructor on its own line (`Money(amount: 1, currency: rub)`) is a "value not used" error.

### 11.4. Errors

An error **does not stop** a command by itself: it is added to the error list. So all validation errors can be collected at once:

```rr
if sum <= 0
  invalidSum(sum: sum)
if frozen
  accountFrozen(accountId: id)
if hasErrors
  return
```

- `return` stops the command.
- Errors of nested calls, including calls to other entities, go to the same list. After a call, `if hasErrors` can skip unneeded work.
- **An error cannot be caught or cancelled.** If there is at least one error in the end, the engine applies none of the effects, and the client gets the error list. The client may be a person, an external system or a timer.
- A forgotten `return` is no disaster: the effects are not applied anyway.

If one entity must react to a situation in another without cancelling everything, the other entity **calls a command** of the first. Errors are addressed to the client.

### 11.5. Fields, `after` and state visibility

- **Fields inside a command are a snapshot** taken when it starts. They do not change until the command ends, even after events are raised. Conditions check the "before" state, and the code is easier to review.
- **`after`** is a snapshot with the events this command has already raised applied. It checks "after" invariants:

```rr
withdrawn(amount: sum)
if after.balance < -overdraft
  insufficientFunds(available: balance + overdraft)
  return
```

- A **nested call** of a command of the same entity, reentrant included (A → B → A), starts from the current state with every event raised so far applied. This is normal behavior.
- Fields of **other** entities are not available in commands. Another entity can be reached only through its commands, and only its existence can be checked.

### 11.6. External requests and splitting commands

A command cannot wait for an external system's answer. The request leaves after commit, and the answer arrives as **a separate call of a public command** ([§18](03-systems-access-ui.md#18-external-systems)). So:

- if the logic needs an external request midway, the command splits in two: "before the request" and "reaction to the answer";
- if data from an external system is needed **before** the command starts, the client gets it through the external integration service and calls the command with the data ready.

---

## 12. Events

1. Assign entity fields the values of arguments or **pure expressions** of arguments and current fields.
2. Are raised from a command by name with named arguments: `orderAppointed(orderId: orderId)`.
3. Event argument names must not coincide with entity field names.
4. An event body consists **only of assignments** to fields; it may be empty, and then the event just records a fact. An event cannot use `if`, loops, calls, rules, `now` or `new`. So an event is deterministic and replayable.
5. Event arguments need not go to fields (for example, `reason` or `at`): they stay part of the event, and the engine decides what to do with them.

```rr
event placed(customerId Customer.Id, orderRoute Route, requestedClass CarClass, orderPrice Money, at timestamp)
  customer = customerId
  route = orderRoute
  carClass = requestedClass
  price = orderPrice
  placedAt = at
  status = searching

event driverSkipped(driverId Driver.Id)
  declinedBy = declinedBy.with(driverId)
  candidate = none
  status = searching
```

> **Decision.** The original rule "events assign fields the values of arguments" is extended to pure expressions (`declinedBy.with(driverId)`). Otherwise every event would have to carry the whole collection.

---

## 13. Notifications

1. Are sent to the client after commit.
2. Are not stored.
3. By default the recipient is the initiator of the root command.
4. An addressed notification: `... to <actor id>`. If the expression is `none`, the notification is not sent.
5. Purpose: tell the client the result of a command.

```rr
notification orderPlaced(orderId Order.Id, price Money)
notification driverOnTheWay(orderId Order.Id)

orderPlaced(orderId: id, price: orderPrice)        # to the initiator
driverOnTheWay(orderId: id) to customer            # to the order's customer (customer is Customer.Id?)
```

A notification without `to` in a command that only a timer can call gives a **compiler warning**: such a command has no human initiator.

**Delivery** is the engine's job (§31.3). Notifications to the initiator come back with the command response. Notifications to other actors go over a subscription channel: in the default engine, WebSocket or long polling. If the recipient is not connected, the notification is not delivered. Long polling keeps a short buffer of the actor's latest notifications in server memory (up to 100, up to 60 s) so they are not lost between two polls. It is a delivery buffer, not storage: after a server restart it is empty.

The engine may also publish notifications to the bus (§31, setting `bus.notifications`): right after commit, to their own channels, without delivery guarantees — like the subscription, this is not storage.

---

## 14. Errors

1. Are sent to the client.
2. Are not stored.
3. May have arguments.
4. Purpose: tell the client that a business rule was broken while running a command or query.

```rr
error offerNoLongerValid
error cannotCancel(currentStatus OrderStatus)
```

**Built-in errors:**

| Error | When |
|---|---|
| `forbidden` | The caller failed the access check |
| `invalidArgument(path string, rule string)` | An API argument failed typing or a `check` |
| `alreadyExists` | A create command (`create`, §10.3): an aggregate with this identifier already exists |
| `internalError` | An execution fault (a bug) |

The engine may add its own infrastructure-level errors.

---

## 15. Timers

A timer is set by the **built-in command** `schedule`. It is the only command that returns a value — the timer identifier:

```rr
let timer = schedule(at: now + 20s, run: expireOffer(driverId: driverId))
offerSent(driverId: driverId, timer: timer)      # keep the timer id in the entity through an event
```

1. Arguments: the run time `at` and a command `run` of the current entity (public or private).
2. `schedule` returns a `Timer.Id`. If the timer may need deleting, its id is kept in a field through an event.
3. Deletion: `delete <Timer.Id or Timer.Id?>`. Deleting a timer that has fired or was deleted, or `none`, is allowed — nothing happens. `delete` has no other forms besides a timer and a projection row (`delete [martId]`).
4. An entity may have any number of timers.
5. Purpose: deferred execution of commands, spreading load, replacing jobs.
6. A timer also sets the deadline of an external request: it is passed as a `timeout Timer.Id` parameter (§18). The timer's command is the reaction to the system not answering, and deleting the timer (the answer arrived) stops the request retries.

Where timers are stored and how they fire is up to the engine.

---

## 16. Projections

1. **An own identifier, like an entity's:** type `OrderMart.Id`. Row access is `[martId]`; the identifier type makes it unambiguous that this is the `OrderMart` projection.
2. Identifier strategies are the same as for an entity (`uuid`, `sequence`, `string`, composite). There is one more: **based on an entity identifier**. A "one row per order" projection declares `id Order.Id`, and then `OrderMart.Id` is a separate nominal type with an `Order.Id` inside. The conversion is explicit both ways: `OrderMart.Id(orderId)` and `Order.Id(martId)`.
3. Fields have default values, as an entity's do.
4. Writing a row (upsert) is a call by name with named arguments: `OrderMart(id: OrderMart.Id(orderId), status: status, placedAt: placedAt)`. **A row is written whole:** every field is passed except optional ones (`T?`) and fields with an explicit `= value`, otherwise error E0305; fields not passed get their default (`none` or the explicit one) rather than keep the previous value. The row is written by the entity that has all its data; data of other entities comes to it as command arguments (in the taxi, `Driver.Accept` passes the driver name and the car plate to `Order.assignDriver`). So the order of writes does not matter: a row has a version — the position of its unit of work — and a write applies only if its version is greater than the stored one. An asynchronous projection applies writes in any order; a repeat is safe.
5. Deletion: `delete [martId]`. A deleted row remembers the deletion version: a late write with a smaller version does not resurrect it.
6. A projection declares indexes (unique ones included) and queries the same way an entity does.
7. Where and how projections are stored is up to the engine.
8. Purpose: fast search over a schema optimized for search, when the search spans data of several entities.

There are no bulk projection updates: a row is updated only by `id`.

---

## 17. Queries

A query is a method of an entity or projection that reads data and returns **only a value object**, a `page<value object>` or a `list<value object>`. The other possible outcome is an error list.

1. **Public** queries (capitalized) are available through the API and UI. **Private** ones (lowercase) — only to other queries, including queries of other entities and projections.
2. A query belongs to the **type**, not to an instance: `Order.Card(orderId: x)`. So one query can work with many instances (search).
3. Inside a query, the data of its **own** type is available:

| Construct | Result |
|---|---|
| `[orderId]` | `Order?` — the record with fields; `none` if the entity does not exist |
| `[Order]` + `where` / `sort` / `page` / `select` | `page<V>` |
| `[Order]` + `where` / `sort` / `limit` / `select` | `list<V>`, `limit` ≤ 100 |
| `[martId]`, `[OrderMart]` | The same for a projection (in projection queries) |
| `Driver.info(driverId: d)` | The result of a query of another type (a value object) |

4. In `where`, `sort` and `select`, bare names are record fields. Query parameters must not share names with them.
5. `where` is a conjunction (`and`) of comparisons of index fields with expressions of parameters. `where` and `sort` use **only the fields of one declared index**. `select` builds a value object from the record. The compiler checks all of this.
6. A query has no effects: commands, events, projections and timers are forbidden.
7. Query errors are declared as `error` of the same entity or projection.
8. Every execution path of a query ends with `return <value>` or a bare `return` **after raising an error**. A bare `return` without an error on its path is a compile error.

```rr
value OrderRow
  orderId Order.Id
  status OrderStatus
  price Money
  placedAt timestamp

aggregate Order
  ...
  index byCustomer(customer, placedAt desc)

  ## My orders, newest first
  query(Customer) MyOrders(pageReq PageRequest) page<OrderRow>
    return [Order]
      where customer == me
      sort placedAt desc
      page pageReq
      select OrderRow(orderId: id, status: status, price: price, placedAt: placedAt)

  ## Order card
  query(Customer) Card(orderId Order.Id) OrderCard
    let o = [orderId]
    if o is none or o.customer != me
      orderNotFound()           # do not reveal whether someone else's order exists
      return
    let driverInfo DriverInfo? = none
    if o.driver is not none
      driverInfo = Driver.info(driverId: o.driver)
    return OrderCard(status: o.status, price: o.price, driver: driverInfo)
```

Fields stay private: an entity exposes only value objects through its queries.
