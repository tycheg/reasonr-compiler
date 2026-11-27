## 18. External systems

1. Requests to external systems are made by the integration service. The engine chooses the protocol, address and format.
2. A request is declared as a static function with parameters.
3. **A request returns nothing** and is sent after commit.
4. An external system answers by **calling a public command** with its own role, like any API client. The role is given in parentheses: `external(matching) Matching`; with it the compiler checks that the system has something to answer with (W1030). If the answering command ends with an error, the external system gets that error.
5. **A request deadline is a model timer.** The optional `timeout` parameter of type `Timer.Id` (or `Timer.Id?`) takes a timer the model has set with `schedule` (§15). The timer time is the delivery deadline, its command is the model's reaction to "no answer came". Once the timer has fired or is deleted (an answer came, the order was cancelled), the engine stops delivering the request. `timeout` is not part of the request body. A request without `timeout` is "fire and forget": the engine retries it up to `integrations.retry.max_elapsed`, then drops it.
6. Complex and long-running algorithms (matching, pricing, scoring) go to separate services and are described as external systems.
7. **The integration service contract** is HTTP and JSON; the engine builds its OpenAPI description from the model (`reasonrd openapi --integrations`, `GET /openapi/integrations.json`). The engine sends `POST {url}/requests/{System}/{request}` with the arguments in canonical JSON (§31.3) and headers: `Authorization: Bearer` with the engine's JWT (ES256; `aud` — the system, `tenant`, `jti` — the idempotency key; public keys — the engine's `GET /.well-known/jwks.json`), `Idempotency-Key` (the same for every attempt), `X-Reasonr-Tenant`, `X-Reasonr-Deadline` (the time of the `timeout` timer), `X-Reasonr-Attempt`. A 2xx response means accepted, and the service carries out the request on its own; any other response is retried with the same key by the `integrations.retry` strategy: while the `timeout` timer is alive, or without a timer up to `max_elapsed`, after which the request is dropped. The system answers with an API command and its own JWT. A service that serves every tenant gets a token with tenant `*` from the IdP (`auth: jwt`, `tenant.any`): the tenant is then taken from `X-Reasonr-Tenant`, and only a token whose roles are all roles of the model's external systems may do so.

```rr
role matching
role payments

external(matching) Matching
  request findDriver(
    orderId Order.Id,
    pickup GeoPoint,
    requestedClass CarClass,
    exclude set<Driver.Id>,
    timeout Timer.Id,
  )

external(payments) Payments
  request charge(paymentId Order.Id, amount Money)
```

```rr
let timer = schedule(at: now + 60s, run: searchTimedOut())
Matching.findDriver(orderId: id, pickup: route.from, requestedClass: carClass, exclude: declinedBy, timeout: timer)
searchStarted(timer: timer)                      # the timer id goes to a field: the answer deletes it
```

### 18.1. Fakes of external systems

A fake is an implementation of an external system in ReasonR for acceptance tests. It behaves like an entity: it has fields, events and commands, and instead of an external API — implementations of the system's requests.

1. A fake is declared as `fake <System>` in `*.fake.rr` files of the base module and is built only into the test program (`reasonrd test`, `reasonrd --test`); the production build and the schema for `--against` do not see it (E1204, E1205).
2. A system has one fake, and the system has a role: `external(role) System` (E1206). The fake calls model commands with that role.
3. There is one fake instance per tenant, without an `id`. Until the fake's first event (as with an entity) it does not exist: requests to the system are only recorded, and the test calls the answers itself. A public command of the fake creates it.
4. Only a test calls the fake's public commands, without `as`: `Matching.Setup(freeDrivers: [d1])` (E1210). Fields change through events, as an entity's do.
5. A `request` in a fake repeats the parameters of the system's request without `timeout`; its body is like a command's (E1207). A request without an implementation is only recorded.
6. A fake sees the request arguments, its own fields and `now`, and sets timers with `schedule`. `[x].Command(…)` in a fake is a call of a public model command through the API with the system's role: the compiler checks access (E1208); it runs after the fake's unit of work commits. Reading entities and projections, `?.`, model queries, external requests, projections, `forget`, `id` and `personal` are not available in a fake (E1209).

```rr
# taxi/contexts/Dispatch/matching.fake.rr

fake Matching
  drivers list<Driver.Id>
  silent bool

  ## Drivers the matching service offers, in order
  command Setup(freeDrivers list<Driver.Id>)
    configured(freeDrivers: freeDrivers)

  ## The matching service stops answering
  command GoSilent()
    silenced()

  event configured(freeDrivers list<Driver.Id>)
    drivers = freeDrivers

  event silenced()
    silent = true

  request findDriver(tripId Trip.Id, pickup GeoPoint, requestedClass CarClass, exclude set<Driver.Id>)
    if silent
      return
    for d in drivers
      if d not in exclude
        [tripId].DriverFound(driverId: d)
        return
    [tripId].NoDriverFound()
```

```rr
# taxi/contexts/Ordering/payments.fake.rr

fake Payments
  limit decimal

  ## Payments up to limitAmount pass, larger ones are declined
  command Setup(limitAmount decimal)
    configured(limitAmount: limitAmount)

  event configured(limitAmount decimal)
    limit = limitAmount

  request charge(paymentId Order.Id, amount Money)
    if amount.amount > limit
      [paymentId].PaymentDeclined(reason: "limit exceeded")
      return
    schedule(at: now + 5s, run: capture(paymentId: paymentId))

  command capture(paymentId Order.Id)
    [paymentId].PaymentCaptured(transactionId: "tx-" + string(paymentId))
```

---

## 19. Access: roles and actors

A **role** is declared at the top level: `role admin`. Roles come from authentication, which the engine performs.

An **actor** (`actor`) is an entity that is a user of the system. Its identifier is the user's identifier in authentication.

Access to a public command, query or app is a list in parentheses; any one condition is enough:

| Element | Who may call | Where it applies |
|---|---|---|
| *(no parentheses)* | Any authenticated caller | everywhere |
| `self` | The actor itself (caller = `id`) | actor commands |
| `<field>` | The actor whose id is in the field, for example `customer` | commands |
| `<Actor>` | Any actor of this type; `me` is available inside | everywhere |
| `<role>` | A user with the role | everywhere |

The compiler resolves a name in parentheses among `self`, fields, actors and declared roles. A role named like a field is a compile error. If access fails, the client gets `forbidden`.

---

## 20. Personal data and crypto-shredding

### 20.1. The idea

The personal data of each **subject** is encrypted with the subject's own key. To "forget" a subject, it is enough to destroy the key: the data becomes unreadable everywhere it went — in state, events, projections, queues, backups. The model only describes **whose data is what**. The engine handles keys, encryption and key deletion.

### 20.2. Markup

The `personal` attribute goes on a field of an entity or projection, or a parameter of an event, command, notification or external request:

| Declaration | Subject |
|---|---|
| `phone string? personal` | The entity itself (`id`) |
| `pickupAddress string? personal(customer)` | The entity whose id is in the field `customer` |
| `(phoneNumber string personal)` in a command | The entity the command is called on |
| `(phoneNumber string? personal)` in an event | The event's entity |
| `driverName string? personal(driverId)` in a projection | The entity from the projection field `driverId` |

Rules:

1. **Personal fields and event parameters are always optional** (`T?`). After crypto-shredding they read as `none`, and the model must handle that.
2. **Leak control (taint).** A value from a personal source can be written only to a personal place: a personal field, event parameter, projection field, external request parameter. The subjects must be of the same type. Writing `phone` to an ordinary field, an ordinary event parameter or an ordinary external request is a **compile error**. Query results and notifications go to authorized clients, so personal data may be returned there. Only **explicit** flows (assignments and arguments) are tracked. Implicit flows through conditions, for example `if phone == "…"` and writing a constant, are not tracked — a known limitation.
3. A command parameter that reaches a personal place must also be marked `personal`, otherwise the compiler warns: the engine must not log such arguments.
4. Indexes on personal fields are **equality only**, unique ones included. Sorting and ranges are forbidden because the engine searches by a blind index.
5. A value object is marked as a whole: `route Route? personal(customer)`.
6. A projection operation that sets a personal field `personal(path)` also sets the subject field (E0906): an asynchronous projection is encrypted before the row is read.
7. The engine encrypts personal data in the arguments of external requests, notifications and command errors with their subjects' keys wherever it stores them (the outbox, the response stored by retry key). The compiler finds the subject: in a command and a reaction, a personal value comes from a field or a `self` parameter, so the subject id is `self` or the `self` field from `personal(...)`. The interpreter passes the subjects in the unit of work (`Subjects`, §30.3).

What crypto-shredding does **not** protect against:

- a compromised application server or anyone with access to the KMS: they have the keys;
- implicit flows (`if phone == …`) the language does not track (rule 2);
- metadata: ids, time, value length, equality of values of one field (equal values have equal blind indexes);
- copies held by recipients: the integration service and clients get plaintext.

### 20.3. Forgetting a subject

```rr
actor Customer
  id uuid
  phone string? personal
  activeOrder Order.Id?

  index byPhone(phone) unique phoneTaken

  ## Delete the customer's personal data
  command(self, support) Forget()
    if activeOrder is not none
      hasActiveOrder()
      return
    forget id
    forgotten()

  event forgotten()          # an empty event — just a fact
```

- `forget <id>` destroys the subject's key in the transaction of its unit of work. No command applies partially: if the same unit of work has an error, the key is not deleted.
- In the same transaction the engine erases the subject's personal fields in rows and its blind values: unique values (a phone number) are free for others at once. All personal fields and event parameters of the subject, wherever they are (the outbox, the event store, asynchronous projections, timers, responses stored by retry key), read as `none`.
- After `forget` the subject may get data again — under a key of a new version; the old data stays unreadable.
- An external request, a timer or a command retry still carrying data of a forgotten subject gets `none`. A request with a non-optional personal parameter is not sent; a timer command with such an argument does not run.
- The event bus gets events without personal arguments: its consumers have no keys.
- A backup of the main database holds the keys of subjects forgotten after it. So the engine keeps a `forget` journal in a separate store. After restoring from a backup, `reasonrd pii replay --since <backup time>` repeats the `forget`s from it (§33).
- The non-personal part of history (statuses, amounts, dates) stays for audit and analytics.

---

## 21. Multitenancy and extensions

### 21.1. Isolation

**Data isolation is the engine's concern.** The model knows nothing about tenants. The engine determines the tenant at authentication and isolates data the chosen way: a `tenant` column with RLS (Row-Level Security — PostgreSQL policies that show a query only the rows of the current tenant at the database level), a schema per tenant or a separate database. Access across tenants is impossible.

**A data tenant and a model with an extension are different things.** A data tenant is the isolation key of rows in stores. A model is the program that runs its commands: the base one or one with an extension (§21.2). The engine configuration says which model a data tenant uses (§31.1). Many data tenants may use one model: for example, `acme-prod` and `acme-staging` with model `acme`, or the ephemeral tenants of tests (§27.3).

### 21.2. Customization: extensions

The base model is shared by everyone. A customer who needs their own fields and behavior gets an **extension** — a customization for that customer: files `extensions/<name>/*.rr`, the extension name is the directory name. A **tenant** is a different notion: a client with its own isolated data (§21.1). The engine configuration says which extension a data tenant has (§31.1); several data tenants may use one extension. For them, **the effective model = base + extension**. The same compiler compiles and tests it, with the same guarantees.

There are three mechanisms.

**1. `extend` — add** without changing the base. An entity or actor can get fields, indexes, commands, events, notifications, errors, queries; a screen — nodes in a part (`in Part`). Base members cannot be changed or removed, and names must not clash. A projection cannot be extended (E1010): the base model writes its row whole (§16) and would drop the extension's fields; an extension keeps its data in its own projection.

**2. `on <event>` — a reaction to a base event.** It runs in the same unit of work after the command that raised the event. A reaction sees the event arguments, the entity fields (read-only) and `after`. A reaction may:

- raise extension events;
- call commands;
- set timers;
- send requests;
- update projections;
- **raise errors** — this is how an extension adds its own checks, and the error cancels the whole base command.

**3. `rule` — override an extension point.** The base model declares a rule — a pure function with a default body. An extension may replace its body, keeping the signature. Rules cannot be called in events, otherwise replaying events would depend on the extension.

```rr
# base model
aggregate Order
  ...
  ## Rate per kilometer. An extension may override it.
  rule perKmRate(requestedClass CarClass) decimal
    if requestedClass == comfort
      return 45
    return 30
```

```rr
# taxi/extensions/acme/contexts/Ordering/order.rr
extend Order
  costCenter string?

  ## A corporate customer sets the cost center
  command(customer) SetCostCenter(code string)
    if code.length == 0
      invalidCostCenter()
      return
    costCenterSet(code: code)

  event costCenterSet(code string)
    costCenter = code

  ## Corporate limit on the ride price
  on placed
    if orderPrice.amount > 5000
      orderLimitExceeded(limit: 5000)

  ## Corporate rate
  rule perKmRate(requestedClass CarClass) decimal
    return 25

  error invalidCostCenter
  error orderLimitExceeded(limit decimal)

extend screen MyOrder
  in Actions
    Edit Order.SetCostCenter
```

`extend screen Screen` adds lines to parts of a screen: `in Actions` — to the only line of part `Actions` on the screen; if there are several such lines, the extension is an error (E1011).

### 21.3. Rules for extensions

- An extension may declare its own entities, projections, queries, screens, apps, primitives and tests.
- **Base tests run for every extension.** If an extension changes behavior on purpose (for example, a limit breaks a base scenario), it excludes the test explicitly, with a reason, visible in review: `exclude test "A customer orders a taxi" because "acme has a 5000 limit"`.
- The engine chooses how to store extension fields (separate tables, columns, jsonb).
- There is one interpreter. The engine keeps a compiled program per extension; the base model bytecode is shared, and the program is chosen by the caller's data tenant.

---

## 22. Interface (UI)

### 22.1. The idea and the levels

The product engineer assembles the interface from **primitives** — display patterns ("a table with filters and pagination") and controls ("a multiselect with autocomplete"). UI developers build primitives: a primitive's interface is declared in the model, its implementation lives on the platform (TypeScript + React, React Native, Swift, Kotlin). The implementation decides how a primitive looks; the model decides which primitives are on a screen and which queries and commands they are bound to.

| Notion | What it is |
|---|---|
| **App** (`app`) | A frontend: who signs in, the menu, screens. Any number of apps over one model: passenger, driver, admin, corporate customer portal |
| **Screen** (`screen`) | A layout: a tree of primitives bound to queries, commands and their fields |
| **Primitive** (`primitive`) | A ready screen block: what it binds to, its parameters, parts and events |

Three levels, one mechanism:

| Level | What the product engineer writes | What for |
|---|---|---|
| Automatic UI | `app … auto` or omitted layout parts — the engine completes them by types (§22.9) | A quick start, trying a feature, a rarely used admin app |
| Layout from primitives | The screens of this section | The main way |
| Own primitive | Nothing: a UI developer builds a primitive for the case, and the layout uses it like any other | What cannot be assembled from ready primitives |

If a primitive is missing, the product engineer **orders** it: writes the declaration (§22.7) or asks a UI developer to. Until the implementation is ready, the layout compiles and the automatic UI is drawn in its place — as an external system runs on a fake until the integration is ready.

A layout has no styles, sizes, loops or arbitrary expressions: only primitives, bindings, labels, display conditions and transitions. Texts and their translations — §22.11. Whatever is missing is a new primitive, not a new language construct.

### 22.2. App

```rr
app(Staff, admin, support) Admin "Admin"
  language en
  platforms web
  default Customer.Id EntityPicker(search: Customer.FindByPhone(phoneNumber: text))
  Menu
    AdminOrders
    NewDriver
    AdminProfile
```

- The parentheses say who signs in, like command access (§19): roles, an actor type. The actor type sets `me` in the app's screens; an actor may have roles too, so `app(Staff, admin, support)` admits staff and callers with roles `admin` and `support`. The string after the name is the app title.
- `language` is the base language: the app's layout strings are written in it (§22.11); `en` by default.
- `platforms` are the app's platforms (`web`, `mobile`); `web` by default. At build time the engine checks that every primitive of the app is implemented on each of its platforms.
- `default Type Primitive(…)` is the default primitive for a type in this app. Top-level `default` declarations (in `primitives/`, §23) apply in every app of the project; the standard ones are in the catalog (§22.8).
- Next comes the layout of the app root, usually the menu. A screen belongs to every app from which it can be reached (menu, transitions, embedding).

### 22.3. Screen

The example is the taxi admin app: an "Orders" section with a list, a card, editing and creating an order, and a "Profile" section with the staff member's data and an email change. The taxi models (§25) need additions for it:

```rr
# taxi/contexts/Ordering/staff.rr

## A staff member
actor Staff
  id uuid
  name string
  email string? personal

  ## Hire a staff member
  command(admin) Hire(staffName string)
    if [id]
      alreadyHired()
      return
    hired(staffName: staffName)

  ## Change the email
  command(self) ChangeEmail(newEmail string personal)
    emailChanged(newEmail: newEmail)

  event hired(staffName string)
    name = staffName

  event emailChanged(newEmail string? personal)
    email = newEmail

  error alreadyHired
  error notStaff

  ## My profile
  query(Staff) Profile() StaffProfile
    let s = [me]
    if s is none
      notStaff()
      return
    return StaffProfile(name: s.name, email: s.email)
```

The additions to the taxi models are in their files (§25.1–25.3):

```rr
# Taxi model additions for the admin app (in full in §25.1–25.3)

# taxi/contexts/Ordering/order.rr — additions

  index byStatus(status, placedAt desc)

  ## Change the route and class while no driver is found
  command(admin, support) Edit(orderRoute Route, requestedClass CarClass)
    if status != searching
      cannotEdit(currentStatus: status)
      return
    let perKm = perKmRate(requestedClass: requestedClass)
    edited(
      orderRoute: orderRoute,
      requestedClass: requestedClass,
      orderPrice: Money(amount: 100 + perKm * decimal(orderRoute.distanceKm), currency: rub),
    )
    writeMart()

  event edited(orderRoute Route, requestedClass CarClass, orderPrice Money)
    route = orderRoute
    carClass = requestedClass
    price = orderPrice

  error cannotEdit(currentStatus OrderStatus)

  ## Orders by status, newest first
  query(admin, support) List(wanted OrderStatus = searching, pageReq PageRequest) page<OrderRow>
    return [Order]
      where status == wanted
      sort placedAt desc
      page pageReq
      select OrderRow(orderId: id, status: status, price: price, placedAt: placedAt)

  ## The whole order
  query(admin, support) Details(orderId Order.Id) OrderDetails
    let o = [orderId]
    if o is none
      orderNotFound()
      return
    return OrderDetails(
      orderId: orderId,
      status: o.status,
      orderRoute: o.route,
      requestedClass: o.carClass,
      price: o.price,
      placedAt: o.placedAt,
      driverName: o.driverName,
    )

  ## Cancelling an order by the customer, support or an administrator
  command(customer, support, admin) Cancel(reason string)

# taxi/contexts/Ordering/customer.rr — staff also place an order for a customer

  command(self, admin, support) OrderTaxi(orderId Order.Id, orderRoute Route, requestedClass CarClass)

  ## Find a customer by phone
  query(admin, support) FindByPhone(phoneNumber string personal) list<CustomerRow>
    return [Customer]
      where phone == phoneNumber
      limit 10
      select CustomerRow(customerId: id, phone: phone)

# taxi/contexts/Ordering/types.rr — additions

value StaffProfile
  name string
  email string?

## Fields are named like the parameters of Order.Edit, so the edit form fills itself
value OrderDetails
  orderId Order.Id
  status OrderStatus
  orderRoute Route
  requestedClass CarClass
  price Money
  placedAt timestamp
  driverName string?

value CustomerRow
  customerId Customer.Id
  phone string?
```

The admin app itself. The app lives in its own directory `apps/Admin/`, its screens in the bounded context, next to the model (§23):

```rr
# taxi/apps/Admin/app.rr

app(Staff, admin, support) Admin "Admin"
  language en
  platforms web
  default Customer.Id EntityPicker(search: Customer.FindByPhone(phoneNumber: text))
  Menu
    AdminOrders
    NewDriver
    AdminProfile
```

```rr
# taxi/contexts/Ordering/admin.rr
# Admin app screens (apps/Admin)

screen AdminOrders "Orders"
  Toolbar
    Link AdminNewOrder
  Table Order.List                               # no Filters written: the wanted filter is automatic
    Columns
      placedAt
      status
      price
    RowActions
      Confirm Order.Cancel if status in [searching, offered, assigned]   # the target is the row's orderId
    on open AdminOrder                                                   # AdminOrder(orderId) ← the row

screen AdminOrder(orderId Order.Id) "Order"
  Card Order.Details                             # orderId ← the screen parameter
    status
    price
    orderRoute
    requestedClass
    placedAt
    driverName
    Actions
      Edit Order.Edit "Edit" if status == searching                               # orderRoute, requestedClass ← card fields
      Confirm Order.Cancel "Cancel" if status in [searching, offered, assigned]   # reason is a confirmation field

screen AdminNewOrder "New order"
  Form Customer.OrderTaxi(orderId: new)          # the customer target is a form field, the app's EntityPicker
    on done AdminOrder                           # AdminOrder(orderId) ← the form values

screen AdminProfile "Profile"
  Card Staff.Profile
    Actions
      Edit Staff.ChangeEmail(newEmail: email) "Change email"   # the names differ, so the link is explicit
```

Acceptance tests of the new commands and queries are like any model's; `--driver web` runs the same tests through the UI (§27.3):

```rr
# taxi/contexts/Ordering/admin.test.rr

let adminRide = Route(from: GeoPoint(lat: 55.75, lon: 37.62), to: GeoPoint(lat: 55.8, lon: 37.6), distanceKm: 10)

test "Staff change the route and class of an order being matched"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000010") as c1
    [c1].OrderTaxi(orderId: o1, orderRoute: adminRide, requestedClass: economy) as c1
  when
    [o1].Edit(orderRoute: adminRide, requestedClass: comfort) as support
  then
    Order.Details(orderId: o1) as support matches (status: searching, requestedClass: comfort)

test "An order with an assigned driver does not change"
  let c1 = new Customer.Id
  let d1 = new Driver.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000011") as c1
    [d1].Register(driverName: "John", driverCar: Car(plate: "B002BB77", model: "Kia Rio", carClass: economy)) as admin
    [d1].GoOnline() as d1
    [c1].OrderTaxi(orderId: o1, orderRoute: adminRide, requestedClass: economy) as c1
    [Trip.Id(o1)].DriverFound(driverId: d1) as matching
    [d1].Accept() as d1
  when
    [o1].Edit(orderRoute: adminRide, requestedClass: comfort) as admin
  then
    error cannotEdit(currentStatus: assigned)

test "Staff place an order for a customer"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000012") as c1
  when
    [c1].OrderTaxi(orderId: o1, orderRoute: adminRide, requestedClass: economy) as admin
  then
    Order.Details(orderId: o1) as admin matches (status: searching, requestedClass: economy)

test "An administrator cancels an order"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000013") as c1
    [c1].OrderTaxi(orderId: o1, orderRoute: adminRide, requestedClass: economy) as c1
  when
    [o1].Cancel(reason: "at the customer's request") as admin
  then
    Order.Details(orderId: o1) as admin matches (status: cancelled)

test "A customer cannot look up other customers by phone"
  let c1 = new Customer.Id
  given
    [c1].SignUp(phoneNumber: "+79990000014") as c1
  when
    Customer.FindByPhone(phoneNumber: "+79990000014") as c1
  then
    error forbidden

test "A staff member changes their email"
  let s1 = new Staff.Id
  given
    [s1].Hire(staffName: "Anna") as admin
  when
    [s1].ChangeEmail(newEmail: "anna@taxi.example") as s1
  then
    Staff.Profile() as s1 matches (name: "Anna", email: "anna@taxi.example")
```

A layout line is one of four:

| Line | What it is | Example |
|---|---|---|
| `Primitive[(parameters)] [binding] ["text"] [if condition]` | A primitive. The binding is a query (`Type.Query(arguments)`), a command (`Type.Command(arguments)` or `[target].Command(arguments)`), a screen (`Screen(arguments)`) or nothing — a layout | `Table Order.List`, `Edit Order.Edit if status == searching`, `Link AdminNewOrder`, `Toolbar` |
| `name [Primitive(parameters)] ["text"] [if condition]` | A field — lowercase, like a field declaration `phone string?`: in data it shows the field, in a form it inputs the parameter. Without a primitive — the default primitive for the type | `price`, `customer EntityPicker(search: Customer.FindByPhone(phoneNumber: text))` |
| `Part` | A part of a primitive that holds the children (§22.7) | `Columns`, `Actions` |
| `on event Screen[(arguments)]` or `on event back` | A transition on a primitive event | `on open AdminOrder`, `on done back` |

A capitalized name is a primitive, a part or a screen: names are globally unique. A screen without a binding inside another screen is **embedding**: its layout is inlined, its parameters are taken by the rules of §22.4. To go to a screen — `Link Screen` or `on event Screen`.

A quoted string is the node label in the app's base language; without it, the label comes from the model texts (§22.11).

### 22.4. Binding

A screen is a hierarchy of **scopes**: screen parameters and `me` → data of the primitive's query → a table or list row → a command form. An inner scope sees everything the outer ones see. Hence the implicit filling rules — the compiler checks them, and `reasonr explain Admin.AdminOrder` shows where each argument comes from:

1. **A query or command argument** — by name:
   1. given explicitly in the binding — an expression (`newEmail: email`, `orderId: new`);
   2. otherwise the same-named value of the nearest scope: a row field, a data field, a screen parameter; another type is an error;
   3. otherwise — **input**: for a query, a filter or a search form; for a command, a form field. A `PageRequest` parameter is filled by the primitive (pagination).
2. **The target of command** `X` — by type: an explicit `[expression]`; otherwise the only value of type `X.Id` in the nearest scope that has one; otherwise `me` if `X` is the app's actor type; otherwise a form field named after the type in lowercase (`customer`) with the default primitive for `X.Id`. Two `X.Id` values in one scope are an error with the fix "set the target explicitly".
3. **Fixed and editable.** Values from screen parameters, `me` and `new` are fixed: the form does not show them. Values from data (card fields, a row) are initial values of form fields and can be changed. So the `Edit Order.Edit` form opens with the current route and class.
4. **Names match through a value object.** A command parameter cannot share a name with a field of its entity (E0103), so the fields of the query's value object are named like the command parameters (`OrderDetails.orderRoute` — `Order.Edit(orderRoute)`). If the names differ — an explicit link `(newEmail: email)`.
5. **Ambiguity** — two same-named values in one scope — is an error with the fix "bind explicitly".
6. **After a command** the screen data is reread; the runtime shows the command's notifications and errors, and an `invalidArgument` error next to the field by `path` (§31.3).

Everything that comes from the client is user input, fixed values included: the command checks access and rules as on any call (§19).

### 22.5. Events and transitions

An event is declared on a primitive (§22.7) and carries data: `open` of a table — the row, `done` of a form — the form values. `on event Screen` opens the screen; its parameters are taken by name — from the event data, then from the scopes (§22.4): `on open AdminOrder` on the order table takes the row's `orderId`. `on event back` returns to the previous screen.

Showing errors and notifications, rereading data and pagination are not events: the primitive and the runtime do them.

### 22.6. Access

1. **The app:** `app(Staff, admin, support)` — who signs in.
2. **A node bound to a query or command** is visible only if the caller has access to the method. Access comes from the model and is not repeated in the layout: the `NewDriver` screen with `Driver.Register` (`command(admin)`) is seen only by a staff member with role `admin`. A screen on which the caller sees nothing disappears from the menu and transitions.
3. **`if`** is a display condition over the scope data. It is a display hint: the command checks the state itself anyway.
4. **An app's API** consists only of the operations of its screens (§31.4): the admin app cannot call a method that is not on its screens.
5. The compiler warns about a node that no one admitted to the app can access and about a screen that cannot be reached.

### 22.7. Primitives

A primitive declaration is written by a UI developer or by the product engineer who orders it. It answers four questions:

| Question | In the declaration |
|---|---|
| What the primitive binds to | `binds` |
| What can be configured | parameters in parentheses, as for a command |
| Which parts it consists of | `children` and `part` |
| What the user can do | `event` |

```rr
## Table: a page of query results with filters and pagination
primitive Table<Row>(size int = 20)
  binds page<Row>                  # a query that returns page<Row>
  part Filters binds params        # query arguments not passed
  part Columns binds Row           # row fields
  part RowActions binds Row        # primitives in every row
  event open(row Row)              # a click on a row

## Card: a query result as "field — value"
primitive Card<V value>
  binds V                          # a query that returns the value object V
  children binds V
  part Actions

## A button that opens a command form in a dialog
primitive Edit
  binds command
  children binds inputs
  event done

## Choosing entities with autocomplete
primitive EntityPicker<R, K id>(
  search(text string) list<R>,     # the candidates query; the primitive passes the typed text to it
  key field<R, K> = auto,          # the row field with the identifier — it goes to the form
  label field<R> = auto,           # the row field the user sees
)
  binds input K | set<K>           # input of one identifier or several
```

**Type variables** `<R, K id>` are blanks the compiler fills by the place in the layout; the product engineer does not write them. After the name comes a constraint, like a type after a field name: `id` — an identifier, `enum` — an enumeration, `value` — a value object; without a constraint — any type. Type variables exist only in primitive declarations: business code has no generics.

**The binding** (`binds`) is what a primitive is put on in a layout:

| Form | What fits | Primitives |
|---|---|---|
| `binds page<Row>`, `binds list<R>`, `binds V` | A query with such a result: a bare type in `binds` is always a query result | `Table`, `List`, `Card` |
| `binds command` | A command: a form or a button | `Form`, `Edit`, `Confirm`, `Button` |
| `binds field T` | A data field — display | `Text`, `Tag`, `RouteMap` |
| `binds input T` | A command or query parameter — input | `Select`, `EntityPicker` |
| `binds screen` | A screen — a transition | `Link` |
| *(none)* | A layout | `Toolbar`, `Row` |

`T` is a language type (`Money`, `string`) or a type variable; `A | B` is one of them. Type variants (`|`) exist only in primitive declarations.

**Parameters** are what a primitive needs besides the binding. `EntityPicker` is bound to an input (what we choose), and its `search` parameter says where to look for candidates. Parameters are declared and passed as for a command; with a default they are optional, `= auto` means the compiler chooses if exactly one option fits.

| Parameter type | What is passed | Example |
|---|---|---|
| A simple type (`int`, `string`, `bool`, an enumeration) | A value | `Table(size: 50)` |
| `list<R>`, `page<R>`, `V` | A query with such a result — as in `binds` | |
| `name(parameters) list<R>` | A query the primitive passes its values to; the query arguments bind to them by name (§22.4) | `search: Customer.FindByPhone(phoneNumber: text)` |
| `field<R>`, `field<R, K>` | The name of a field of row `R` — any, or of type `K` | `label: phone` |
| `screen` | A screen | |

**Parts.** A complex primitive consists of zones: a table has filters, columns and row actions. `part Name binds what` declares a part and what lowercase names inside it mean; `children binds what` does the same for children written right under the primitive:

| `binds` | A name inside the part is | Example |
|---|---|---|
| a type variable | a field of a value of that type; if it is a row of the bound page or list, the part repeats in every row | `Columns`, `RowActions` of `Table`; children of `Card` |
| `params` | an argument of the bound query not passed | `Filters` of `Table` |
| `inputs` | an input of a parameter of the bound command | children of `Form`, `Edit`, `Confirm` |
| `screens` | a screen — a navigation item | children of `Menu`, `Tabs` |
| *(none)* | primitives only | `Actions` of `Card`, `Toolbar` |

Primitives may go in any part. So `status` in `Columns` is the field `OrderRow.status`, while `status` in `Filters` would be a query argument: the part sets the meaning of a name.

An omitted part that binds data fills automatically: all arguments not passed, all fields, all inputs. The value fields are shown by the first part of the primitive that binds them (children before parts): `RowActions` after `Columns` binds the same row fields only as a scope. A part of primitives only (`Actions`, `RowActions`) without lines is empty.

**Events** — `event name(parameters)`: `event open(row Row)`, `event picked(point GeoPoint)`. The `done` event of a primitive with `binds command` carries the form values. In a layout, a transition is put on an event (§22.5).

**What the compiler infers and checks** — on the field `customer EntityPicker(search: Customer.FindByPhone(phoneNumber: text))` of the `AdminNewOrder` screen:

| Blank | From | Value |
|---|---|---|
| `K` | the type of input `customer` — the target of `Customer.OrderTaxi` | `Customer.Id` |
| `R` | the result of `FindByPhone` — `list<CustomerRow>` | `CustomerRow` |
| `key = auto` | the only field of `CustomerRow` of type `Customer.Id` | `customerId` |
| `label = auto` | the first string field of `CustomerRow` | `phone` |

Errors: the result row has no field of type `K` ("set `key` or use another query"); `EntityPicker` is put on an input that is not an identifier; a query argument is neither passed nor bound to `text`.

**Implementation.** An interface for each platform is generated from the declaration; there is no model in it — the runtime has substituted everything:

```tsx
// web: from primitive EntityPicker
export interface EntityPickerProps<R, K> {
  value: K[];                          // the chosen ones; at most one for an input K
  multiple: boolean;                   // K or set<K>
  onChange(value: K[]): void;
  search(text: string): Promise<R[]>;  // the runtime calls FindByPhone(phoneNumber: text)
  key(row: R): K;                      // row => row.customerId
  label(row: R): string;               // row => row.phone
  rr: RR;                              // labels for UI tests (§22.10)
}
```

The look, the component library and changing it are inside the implementations. Implementations change one at a time, so some primitives may be on one library and some on another while the layout stays the same.

### 22.8. The standard catalog

The catalog is declared in the compiler (file `std/ui.rr`, implicitly part of every module) and implemented by the engine's UI developers; a module adds its own primitives. A model type may be named like a catalog primitive (`value Row`): a type and a primitive stand in different places of the code. A screen, an app or an own primitive named like a catalog one is an error.

| Group | Primitives |
|---|---|
| Navigation and layout | `Menu`, `Tabs` (`children binds screens`), `Link` (`binds screen`), `Toolbar`, `Row`, `Stack`, `Section` |
| Data | `Table<Row>` (`binds page<Row>`; `Filters`, `Columns`, `RowActions`, `open`), `List<Row>` (`binds page<Row> \| list<Row>`; children per row, `RowActions`, `open`), `Card<V value>` (`binds V`; children are fields, `Actions`) |
| Field display | `Text<T>`, `Tag<E enum>`, `DateTime` (`timestamp`), `Fields<V value>` (nested fields), `Items<T>` (`list<T> \| set<T>`) — `binds field` |
| Commands | `Form` (on the screen), `Edit` (a button and a form in a dialog), `Confirm` (a button and a confirmation; parameters not passed go to the dialog), `Button` (all parameters set) — `binds command`, children `inputs`, event `done` |
| Input | `TextInput` (`string`), `NumberInput` (`int \| bigint \| decimal \| float`), `Checkbox` (`bool`), `Select<E enum>`, `MultiSelect<E enum>` (`set<E>`), `DateInput`, `TimeInput`, `DateTimeInput`, `DurationInput`, `EntityPicker<R, K id>`, `Group<V value>` (children are fields), `ListInput<T>` (`list<T> \| set<T>`) — `binds input` |

Default primitives for types — `default type Primitive(parameters)`; instead of a type, a constraint (`enum`, `value`, `id`) or `set<enum>` may be written:

```rr
default string Text
default string TextInput
default enum Tag
default enum Select
default set<enum> MultiSelect
default timestamp DateTime
default timestamp DateTimeInput
default value Fields
default value Group
default id Text
```

There are two for one type: display (`binds field`) and input (`binds input`) — the right one is chosen by place. An identifier input without its own `default` is an error: identifiers are not typed by hand, a picker primitive is needed:

```rr
default Customer.Id EntityPicker(search: Customer.FindByPhone(phoneNumber: text))
```

### 22.9. Automatic UI

**Omitted parts.** A bound primitive without children fills itself: `Table Order.List` — filters by the arguments not passed, columns by all row fields; `Form Customer.OrderTaxi(orderId: new)` — fields by all inputs; a field without a primitive — the default primitive for the type.

**A whole app** — `app(Staff, admin, support) Admin auto`, without a layout:

1. The public queries and commands available to those admitted to the app are taken.
2. For each type with a page query or a list query without identifier parameters — a screen in the menu: a `Table` or `List` per such query, a `Card` per value query without identifier parameters (for example, a profile).
3. For each type `X` with a value query that has an `X.Id` parameter — a card screen with an `X.Id` parameter: a `Card` per such query, and in `Actions` an `Edit` or `Button` for each command of `X` whose target is that parameter.
4. Rows with an `X.Id` field open the card of `X` (`on open`).
5. Commands whose target is `me` go to `Actions` of the profile screen; commands whose target or identifier parameter cannot be taken from a scope — a `Form` in the menu if the identifier has a `default`, otherwise the parameter gets `new` (creation).
6. A command with no place to go does not get into the app; `reasonr explain Admin` lists such commands.

`reasonr ui eject Admin` prints the layout `auto` builds — a convenient start for your own. In a project with parts (§23) the layout is split across files: the app — `apps/Admin/app.rr`, screens — in the bounded context of the type whose methods they are bound to (`contexts/Ordering/admin.rr`).

### 22.10. Places and labels for UI tests

A **place** is a layout node bound to a query or command: `Admin.AdminOrder:Order.Edit`; a second node with the same method on a screen is `#2`. The compiler lists places; the runner calls test steps through the UI by them (§27.3).

The runtime sets labels itself; a primitive implementation only puts the `rr` it gets on its elements: on the web — `data-rr`, on mobile — `testID` / accessibility id.

| Element | Label |
|---|---|
| Screen | `screen:Admin.AdminOrder` |
| Place (the node root) | `place:Admin.AdminOrder:Order.Edit` |
| Input | `input:Admin.AdminOrder:Order.Edit.orderRoute` |
| Command submit | `submit:Admin.AdminOrder:Order.Edit` |
| Query result, with the serialized value | `result:Admin.AdminOrder:Order.Details` |
| Result row | `item:Admin.AdminOrders:Order.List:<id>` |
| Notification, error | `notification:orderPlaced`, `error:cannotCancel` |

In development mode the engine checks that primitive implementations set every label.

### 22.11. Texts and languages

`##` is code documentation (`explain`, LSP hints); the user does not see it. Interface texts are set in two ways:

1. **A string in the layout** — a label in the app's base language (`language en`): `app(Staff, admin, support) Admin "Admin"`, `screen AdminOrders "Orders"`, `Edit Order.Edit "Edit"`.
2. **A `texts` block** — model texts and translations. The key is a path to a model name or a layout node:

```rr
# taxi/contexts/Ordering/texts.en.rr
# Model texts in the apps' base language

texts en
  Order.Cancel "Cancel order"
  Order.cannotCancel "Cannot cancel an order with status {currentStatus}"
  Order.cannotEdit "Cannot change an order with status {currentStatus}"
  Order.driverOnTheWay "The driver is on the way"
  Order.orderPlaced "Order placed, price {price}"
  Order.LeaveTip "Leave a tip"
  Order.tipsDisabled "Tips are not available yet"
  Order.cannotTip "Tips come after the ride is paid, now {currentStatus}"
  OrderStatus.searching "Looking for a driver"
  OrderStatus.offered "Offered to a driver"
  OrderStatus.assigned "The driver is on the way"
  OrderStatus.completed "Ride completed"
  OrderStatus.paid "Paid"
  OrderStatus.unpaid "Not paid"
  OrderStatus.cancelled "Cancelled"
  OrderStatus.noDrivers "No drivers"
  OrderRow.placedAt "Placed"
  Customer.OrderTaxi.requestedClass "Class"
  Customer.OrderTaxi.orderRoute "Route"
```

```rr
# A German translation — an example; the taxi has none
texts de
  Admin "Verwaltung"
  AdminOrders "Bestellungen"
  AdminOrder:Order.Edit "Bearbeiten"
  Order.Cancel "Bestellung stornieren"
  Order.cannotCancel "Eine Bestellung im Status {currentStatus} kann nicht storniert werden"
  OrderStatus.searching "Fahrer wird gesucht"
```

| Key | What it labels |
|---|---|
| `Type`, `Type.member`, `Type.Method.parameter`, `Enumeration.value` | A type, field, command, query, error, notification, parameter, enumeration value — wherever they are shown |
| `App`, `Screen` | The app title, the screen title |
| `Screen:binding` | A layout node: `AdminOrder:Order.Edit`, `AdminOrders:AdminNewOrder`; a node without a binding — `Screen:Primitive` (`AdminOrders:Toolbar`), a part — `Screen:Part`, a field line — `Screen:field`; a second node with the same key on a screen — `#2` |

A label in the user's language is looked up as: `texts` of that language → the layout string or `texts` of the base language → the name split into words (`placedAt` → "Placed at"). So the automatic UI works without texts too.

The compiler checks that a key refers to an existing name or node and that `{…}` substitutions refer to error or notification parameters, and warns about base-language texts missing from another language of the model.
