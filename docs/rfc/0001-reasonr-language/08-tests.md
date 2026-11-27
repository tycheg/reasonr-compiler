## 27. Acceptance tests

An acceptance test checks a business requirement through the system's external interfaces, as a black box: public commands and queries, notifications, external requests. It is written as a `given` / `when` / `then` scenario. The test does not know how the model works inside, so one test checks the model in memory, the engine through the API and the apps through the UI (§27.3).

### 27.1. Rules

Tests are written in the same language, in `*.test.rr` files. They check the system **only through the public interface**, so the same tests run on different drivers (§27.3): in memory, through the API and through the UI.

| Section | What is allowed |
|---|---|
| `given` | **Public commands** (on behalf of an actor or role), `time offset ±<duration>` and the statements `let`, assignment, `for`, `while`, `if` with steps in their blocks |
| `when` | One **public command**, one **public query** or `time offset ±<duration>`; `let` before it |
| `then` | Expected errors, notifications, the query result, sent external requests, results of other public queries |

- `as <actor or role>` — on whose behalf the call is made.
- Command and query arguments in a test are as for an API client: an omitted parameter gets its default, `PageRequest` — `size 20`.
- **Time.** A test runs on the current time: the clock starts at the moment of the run, not at a made-up date. `time offset +20s` moves the scenario clock forward — every timer whose time has come fires; `time offset -30d` moves it into the past, and timers do not fire. Offsets add up. The old form `time passes 20s` is parsed, and `reasonr fmt` writes it as `time offset +20s`. Time is compared with the scenario's `now`, not with dates.
- **Variables and loops.** `given` allows `let`, assignment, `for`, `while`, `if`; in their blocks a call is a step (`[c].SignUp(…) as c`), `time offset` is a shift. `range(n)` is the numbers from 0 to n−1.
- **`faker`** — plausible values for tests and demos (an error in the model, E0222): `faker.firstName()`, `lastName()`, `name()`, `phone()`, `email()`, `address()`, `company()`, `text(words)`, `int(a, b)`, `decimal(a, b)`, `float(a, b)`, `chance(p)`, `pick(list)`, `date(from, to)`, `pattern("?###??77")` (`#` — a digit, `?` — a letter). Values are deterministic: the seed comes from the test name and `--seed`; `phone` and `email` do not repeat within a run.
- External requests are recorded. If the system has a fake (§18.1) and the test has created it, the fake carries out the request; otherwise the test calls the system's answer itself — a public command with its role.
- **A fake cascade** finishes inside the step: `then` sees the notifications and external requests of every unit of work of the cascade ("contains"), and errors are compared only for the command of the `when` step. An error of a command a fake called fails the test (kind `fake`), like an error of a `given` step. More than 1000 units of work in a step's cascade is an execution fault: the fake and the model call each other endlessly.
- `matches (field: value, ...)` is a partial comparison of a value object: only the written fields are checked, nested value objects the same way.
- **Errors are checked exactly:** the errors of the `when` step must equal those listed in `then` as a set. An unexpected error fails the test: an error cancels all the effects, and it is almost always a bug.
- **Notifications and external requests — "contains":** each expected one must be among those sent by the `when` step; extra ones are fine. Only the written arguments are compared.
- An error in a `given` step fails the test, naming the step.
- Shared fixtures are declared with `let` at the top level of a test file.
- `feature tips[, …]` — the first line of a test: flags switched on for everyone (§23.4). Without it all flags are off: old tests check the behavior before the rollout.

### 27.2. Examples

```rr
# taxi/contexts/Ordering/order.test.rr

let car1 = Car(plate: "A001AA77", model: "Kia Rio", carClass: economy)
let center = GeoPoint(lat: 55.75, lon: 37.62)
let ride = Route(from: center, to: center, distanceKm: 3)

test "The driver accepts the offer and the order is assigned"
  let c1 = new Customer.Id
  let d1 = new Driver.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000000") as c1
    [d1].Register(driverName: "John", driverCar: car1) as admin
    [d1].GoOnline() as d1
    [c1].OrderTaxi(orderId: o1, orderRoute: ride, requestedClass: economy) as c1
    [Trip.Id(o1)].DriverFound(driverId: d1) as matching
  when
    [d1].Accept() as d1
  then
    notification driverOnTheWay(orderId: o1) to c1
    Order.Card(orderId: o1) as c1 matches (status: assigned)
    Driver.MyState() as d1 matches (status: busy, currentTrip: Trip.Id(o1))

test "A busy driver: the order goes to the next driver"
  let c1 = new Customer.Id
  let c2 = new Customer.Id
  let d1 = new Driver.Id
  let o1 = new Order.Id
  let o2 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000001") as c1
    [c2].SignUp(phoneNumber: "+79990000002") as c2
    [d1].Register(driverName: "John", driverCar: car1) as admin
    [d1].GoOnline() as d1
    [c1].OrderTaxi(orderId: o1, orderRoute: ride, requestedClass: economy) as c1
    [Trip.Id(o1)].DriverFound(driverId: d1) as matching
    [d1].Accept() as d1
    [c2].OrderTaxi(orderId: o2, orderRoute: ride, requestedClass: economy) as c2
  when
    [Trip.Id(o2)].DriverFound(driverId: d1) as matching
  then
    request Matching.findDriver(tripId: Trip.Id(o2), exclude: [d1])
    Order.Card(orderId: o2) as c2 matches (status: searching)
    Driver.MyState() as d1 matches (status: busy, currentTrip: Trip.Id(o1))

test "A search without an answer restarts"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000000") as c1
    [c1].OrderTaxi(orderId: o1, orderRoute: ride, requestedClass: economy) as c1
  when
    time offset +60s
  then
    request Matching.findDriver(tripId: Trip.Id(o1))
    Order.Card(orderId: o1) as c1 matches (status: searching)

test "The car plate is unique"
  let d1 = new Driver.Id
  let d2 = new Driver.Id
  given
    [d1].Register(driverName: "John", driverCar: car1) as admin
  when
    [d2].Register(driverName: "Peter", driverCar: car1) as admin
  then
    error plateTaken

test "After forgetting personal data the phone is unreadable"
  let c1 = new Customer.Id
  given
    [c1].SignUp(phoneNumber: "+79990000000") as c1
  when
    [c1].Forget() as c1
  then
    Customer.Profile() as c1 matches (phone: none)

test "Another customer may take a forgotten customer's number"
  let c1 = new Customer.Id
  let c2 = new Customer.Id
  given
    [c1].SignUp(phoneNumber: "+79990000000") as c1
    [c1].Forget() as c1
  when
    [c2].SignUp(phoneNumber: "+79990000000") as c2
  then
    Customer.Profile() as c2 matches (phone: "+79990000000")
    Customer.Profile() as c1 matches (phone: none)

test "Forgetting with an active order erases nothing"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000000") as c1
    [c1].OrderTaxi(orderId: o1, orderRoute: ride, requestedClass: economy) as c1
  when
    [c1].Forget() as c1
  then
    error hasActiveOrder
    Customer.Profile() as c1 matches (phone: "+79990000000")

test "Another customer's order is not visible"
  let c1 = new Customer.Id
  let c2 = new Customer.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000001") as c1
    [c2].SignUp(phoneNumber: "+79990000002") as c2
    [c1].OrderTaxi(orderId: o1, orderRoute: ride, requestedClass: economy) as c1
  when
    Order.Card(orderId: o1) as c2
  then
    error orderNotFound
```

End-to-end taxi scenarios on fakes of the matching and payment systems:

```rr
# taxi/contexts/Ordering/ride.test.rr
# End-to-end scenarios: the matching and payment services are replaced by
# fakes (matching.fake.rr, payments.fake.rr); the test does not call the
# systems' answers itself.

let kia = Car(plate: "B002BB77", model: "Kia Rio", carClass: economy)
let lada = Car(plate: "E003EE77", model: "Lada Vesta", carClass: economy)
let point = GeoPoint(lat: 55.75, lon: 37.62)
let trip = Route(from: point, to: point, distanceKm: 3)

test "A ride on fakes: matching, the ride, payment"
  let c1 = new Customer.Id
  let d1 = new Driver.Id
  let o1 = new Order.Id
  given
    Matching.Setup(freeDrivers: [d1])
    Payments.Setup(limitAmount: 1000)
    [c1].SignUp(phoneNumber: "+79990000010") as c1
    [d1].Register(driverName: "John", driverCar: kia) as admin
    [d1].GoOnline() as d1
    [c1].OrderTaxi(orderId: o1, orderRoute: trip, requestedClass: economy) as c1
    [d1].Accept() as d1
    [d1].FinishRide() as d1
  when
    time offset +5s
  then
    notification receipt(orderId: o1) to c1
    Order.Card(orderId: o1) as c1 matches (status: paid)

test "A customer tips the driver"
  feature tips
  let c1 = new Customer.Id
  let d1 = new Driver.Id
  let o1 = new Order.Id
  given
    Matching.Setup(freeDrivers: [d1])
    Payments.Setup(limitAmount: 1000)
    [c1].SignUp(phoneNumber: "+79990000013") as c1
    [d1].Register(driverName: "John", driverCar: kia) as admin
    [d1].GoOnline() as d1
    [c1].OrderTaxi(orderId: o1, orderRoute: trip, requestedClass: economy) as c1
    [d1].Accept() as d1
    [d1].FinishRide() as d1
    time offset +5s
  when
    [o1].LeaveTip(amount: Money(amount: 50, currency: rub)) as c1
  then
    notification tipReceived(tripId: Trip.Id(o1)) to d1
    Order.Card(orderId: o1) as c1 matches (status: paid, tip: Money(amount: 50, currency: rub))

test "No tips before payment"
  feature tips
  let c1 = new Customer.Id
  let d1 = new Driver.Id
  let o1 = new Order.Id
  given
    Matching.Setup(freeDrivers: [d1])
    [c1].SignUp(phoneNumber: "+79990000014") as c1
    [d1].Register(driverName: "John", driverCar: kia) as admin
    [d1].GoOnline() as d1
    [c1].OrderTaxi(orderId: o1, orderRoute: trip, requestedClass: economy) as c1
    [d1].Accept() as d1
  when
    [o1].LeaveTip(amount: Money(amount: 50, currency: rub)) as c1
  then
    error cannotTip(currentStatus: assigned)

test "A payment over the limit is declined"
  let c1 = new Customer.Id
  let d1 = new Driver.Id
  let o1 = new Order.Id
  given
    Matching.Setup(freeDrivers: [d1])
    Payments.Setup(limitAmount: 100)
    [c1].SignUp(phoneNumber: "+79990000011") as c1
    [d1].Register(driverName: "John", driverCar: kia) as admin
    [d1].GoOnline() as d1
    [c1].OrderTaxi(orderId: o1, orderRoute: trip, requestedClass: economy) as c1
    [d1].Accept() as d1
  when
    [d1].FinishRide() as d1
  then
    notification paymentProblem(orderId: o1, reason: "limit exceeded") to c1
    Order.Card(orderId: o1) as c1 matches (status: unpaid)

test "No free drivers"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    Matching.Setup(freeDrivers: [])
    [c1].SignUp(phoneNumber: "+79990000012") as c1
  when
    [c1].OrderTaxi(orderId: o1, orderRoute: trip, requestedClass: economy) as c1
  then
    notification noDriversAvailable(orderId: o1) to c1
    Order.Card(orderId: o1) as c1 matches (status: noDrivers)

test "The first driver is busy: the fake offers the next one"
  let c1 = new Customer.Id
  let c2 = new Customer.Id
  let d1 = new Driver.Id
  let d2 = new Driver.Id
  let o1 = new Order.Id
  let o2 = new Order.Id
  given
    Matching.Setup(freeDrivers: [d1, d2])
    [c1].SignUp(phoneNumber: "+79990000013") as c1
    [c2].SignUp(phoneNumber: "+79990000014") as c2
    [d1].Register(driverName: "John", driverCar: kia) as admin
    [d2].Register(driverName: "Peter", driverCar: lada) as admin
    [d1].GoOnline() as d1
    [d2].GoOnline() as d2
    [c1].OrderTaxi(orderId: o1, orderRoute: trip, requestedClass: economy) as c1
    [d1].Accept() as d1
  when
    [c2].OrderTaxi(orderId: o2, orderRoute: trip, requestedClass: economy) as c2
  then
    request Matching.findDriver(tripId: Trip.Id(o2), exclude: [d1])
    Order.Card(orderId: o2) as c2 matches (status: offered)
    Driver.MyState() as d2 matches (status: offered)

test "The matching service is silent: no drivers after three attempts"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    Matching.GoSilent()
    [c1].SignUp(phoneNumber: "+79990000015") as c1
    [c1].OrderTaxi(orderId: o1, orderRoute: trip, requestedClass: economy) as c1
  when
    time offset +240s
  then
    notification noDriversAvailable(orderId: o1) to c1
    Order.Card(orderId: o1) as c1 matches (status: noDrivers)

test "Cancelling an order while matching is silent"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    Matching.GoSilent()
    [c1].SignUp(phoneNumber: "+79990000016") as c1
    [c1].OrderTaxi(orderId: o1, orderRoute: trip, requestedClass: economy) as c1
    [o1].Cancel(reason: "changed my mind") as c1
  when
    time offset +60s
  then
    Order.Card(orderId: o1) as c1 matches (status: cancelled)
```


### 27.3. Running on different drivers

```bash
reasonrd test                     # memory: like unit tests, milliseconds; the base model and every extension
reasonrd test --driver api        # each command is a POST, each query a GET to the API of a test mode server
reasonrd test --driver web        # a browser (Playwright): commands and queries through the UI
reasonrd test --driver mobile     # an emulator (Detox / Appium): the same for the mobile app
reasonrd test --driver api,web    # a pipeline: each test runs through the API and through the web places
reasonrd test --extension acme       # only the effective model with the extension: base tests + extension tests (base — the base model only)
reasonrd test order.test.rr:58    # one test — the one containing line 58; order.test.rr — every test of the file
```

| Driver | Command | Query | What is checked |
|---|---|---|---|
| `memory` | A direct interpreter call, every entity in the test's memory | A direct call | Business logic |
| `api` | `POST /api/...` with the actor's or role's token | `GET /api/...` | Logic + API, serialization, authorization |
| `web` | The browser opens the command's place on a screen, fills the inputs, submits | Opens the query's place, reads the result | Logic + API + UI |
| `mobile` | The same in the mobile app | The same | Logic + API + mobile UI |

**A list of drivers.** `--driver` takes a comma-separated list: a test runs on the drivers in order — once on `memory` and `api`, k times on a UI driver (by places, see below). Failures in the report carry the run's driver (`driver`, except `memory`) and the test run number (`run`, from the second one).

**The test mode server.** Drivers `api`, `web` and `mobile` work with a server in test mode (`reasonrd --test`, only explicitly). Without `--url` the engine starts one itself on a free address — on the test's model and the `--config` adapters (without it, everything in memory); with `--url` the tests run against a server already running. The test API `/_test/*` creates an **ephemeral data tenant** for each test with its own model and virtual clock. So tests run in parallel in one database without interfering. Through the same API the runner moves the clock (`time offset`; timers fire synchronously, and the response holds their errors, notifications and external requests) and gets the recorded external requests. In test mode the actor or role is set with the header `Authorization: Test actor=Customer:<id>` or `Test role=admin`. The runner passes queries with `after` (§31.3): tests pass the same way with asynchronous projections. The server address is `--url`, the number of concurrent tests is `--parallel`.

**Workers.** Several machines run a large test suite: the coordinator `reasonrd test --serve [--listen :7070]` with the usual flags (`--driver`, `--run`, `--affected`) makes "model test × drivers" jobs and keeps the queue; workers `reasonrd test-worker --coordinator http://…:7070 [--drivers memory,api,web] [--slots 8] [--url address]` pull jobs themselves: faster ones take more, and late ones join in too. A job is leased and the lease is extended while the worker runs it; if a worker disappears, the job goes back to the queue, and after two retries it fails with kind `fault` "the worker returned no result". A worker builds the program from its own copy of the module (in CI — the same repository) and checks the source hash against the job; for drivers `api` and `web` it starts its own test mode server. The coordinator report is the same as a local run's. The shared token is `REASONR_TEST_TOKEN`.

```sh
reasonrd test --serve --listen :7070 --driver api,web --format json examples/taxi/src > report.json   # the coordinator
reasonrd test-worker --coordinator http://ci-coord:7070 --drivers api --slots 16                    # workers on CI machines
reasonrd test-worker --coordinator http://ci-coord:7070 --drivers web --slots 4
```

**UI drivers (`web`, `mobile`).** An acceptance test does not know about screens: the same test runs on every driver. What the test checks is checked through the UI, and the setup bypasses the UI:

| Section | How it runs |
|---|---|
| `given` | Through the test API of the same server, as for driver `api`: it prepares state, it does not check the UI |
| `when` | A command — through a place on a screen; `time passes` and answers of external systems (`as matching`) — through the test API |
| `then` | Errors and notifications of the `when` step — as shown in the UI (`error:*`, `notification:*`); queries — through the query's place on a screen; notifications to other actors and external requests — through the test API |

**Places and k runs.** One command or query can be called from several places (§22.10): `Order.Cancel` in the admin app — in a list row and in the card. The runner checks each place, but not each combination of places:

1. For a step, the **applicable** places are taken: the app is available to the caller, the node is visible in the current state (`if`), and the place's fixed values (§22.4) equal the step arguments.
2. If the test's steps have up to k applicable places, the test runs k times, each time in a new ephemeral tenant: in run i, a step takes its i-th place (round robin if it has fewer). The number of runs is the maximum number of places of one step, not their product.
3. No applicable place fails the test with "the action is not available in the UI" (kind `ui`): this catches, for example, a button hidden by a wrong `if`. The exception is a `when` step from which the test expects errors: see below.
4. `--coverage` shows the places no test reached; `--place Admin.AdminOrder:Order.Edit` runs only through that place (for debugging).

**How the runner runs a step through a place.** For `[o1].Cancel(reason: "changed my mind") as a1` at the place `Admin.AdminOrder:Order.Cancel`:

1. Signs in as `a1` and opens the screen by its direct address: the screen parameters come from the step arguments by the place's links (`orderId = o1`). For a place in a list row (`Admin.AdminOrders:Order.Cancel`) the runner opens the list and finds the row `item:…:o1`, paging through. With `--navigate path` the runner reaches the screen from the menu by clicks — checking the navigation as well.
2. Writes the step arguments into the place's inputs (over the initial values) and presses `submit:…`.
3. Compares the errors and notifications shown with the `then` section.

The runner runs a query the same way: opens the query's place, enters the filters and reads the value from `result:…`.

A step without an applicable place and a step whose errors the test expects are compatible: if `then` lists errors of the `when` step and the place is hidden (`if`), the UI rightly did not allow the action, and the test passes. If the place is shown, the command goes to the server and the errors are compared as usual.

For `--coverage` to pass every place, each place needs a step. Taxi tests that reach places without their own scenarios:

```rr
# taxi/contexts/Ordering/ui.test.rr
# Actions and queries that have places in the apps (§22): with
# reasonrd test --driver web they go through the interface (§27.3).

let car = Car(plate: "K004KK77", model: "Kia Rio", carClass: economy)
let here = GeoPoint(lat: 55.75, lon: 37.62)
let hop = Route(from: here, to: here, distanceKm: 2)

test "A driver goes online"
  let d1 = new Driver.Id
  given
    [d1].Register(driverName: "John", driverCar: car) as admin
  when
    [d1].GoOnline() as d1
  then
    Driver.MyState() as d1 matches (status: free)

test "A driver goes offline"
  let d1 = new Driver.Id
  given
    [d1].Register(driverName: "John", driverCar: car) as admin
    [d1].GoOnline() as d1
  when
    [d1].GoOffline() as d1
  then
    Driver.MyState() as d1 matches (status: offline)

test "A driver declines an offer"
  let c1 = new Customer.Id
  let d1 = new Driver.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000020") as c1
    [d1].Register(driverName: "John", driverCar: car) as admin
    [d1].GoOnline() as d1
    [c1].OrderTaxi(orderId: o1, orderRoute: hop, requestedClass: economy) as c1
    [Trip.Id(o1)].DriverFound(driverId: d1) as matching
  when
    [d1].Decline() as d1
  then
    request Matching.findDriver(tripId: Trip.Id(o1), exclude: [d1])
    Driver.MyState() as d1 matches (status: free)

test "A passenger cancels their order"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000021") as c1
    [c1].OrderTaxi(orderId: o1, orderRoute: hop, requestedClass: economy) as c1
  when
    [o1].Cancel(reason: "changed my mind") as c1
  then
    Order.Card(orderId: o1) as c1 matches (status: cancelled)

test "A passenger opens their orders"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000022") as c1
    [c1].OrderTaxi(orderId: o1, orderRoute: hop, requestedClass: economy) as c1
  when
    Order.MyOrders() as c1
  then
    Order.Card(orderId: o1) as c1 matches (status: searching)

test "Staff open the orders being matched"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000023") as c1
    [c1].OrderTaxi(orderId: o1, orderRoute: hop, requestedClass: economy) as c1
  when
    Order.List() as admin
  then
    Order.Details(orderId: o1) as admin matches (status: searching)

test "The dispatcher opens the order board"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000024") as c1
    [c1].OrderTaxi(orderId: o1, orderRoute: hop, requestedClass: economy) as c1
  when
    OrderMart.Board(wanted: searching) as dispatcher
  then
    Order.Details(orderId: o1) as admin matches (status: searching)

test "The dispatcher opens the free drivers"
  let d1 = new Driver.Id
  given
    [d1].Register(driverName: "John", driverCar: car) as admin
    [d1].GoOnline() as d1
  when
    Driver.FreeDrivers(wantedClass: economy) as dispatcher
  then
    Driver.MyState() as d1 matches (status: free)
```

```rr
# taxi/extensions/acme/contexts/Ordering/order.test.rr
let spot = GeoPoint(lat: 55.75, lon: 37.62)

test "An empty cost center is rejected"
  let c1 = new Customer.Id
  let o1 = new Order.Id
  given
    [c1].SignUp(phoneNumber: "+79990000030") as c1
    [c1].OrderTaxi(orderId: o1, orderRoute: Route(from: spot, to: spot, distanceKm: 2), requestedClass: economy) as c1
  when
    [o1].SetCostCenter(code: "") as c1
  then
    error invalidCostCenter
```

A test calls a page query as a client does: `PageRequest` may be omitted, `size 20` by default (§7.2). `matches` compares only value objects, so the result of a list query is checked by the UI (the place is shown, the value is displayed), and the state by a query in `then`.

### 27.4. Affected tests

A change runs only the tests whose result it may affect: `reasonr test --affected [--base origin/main]` (also `reasonrd test --affected`). This is test impact analysis, and it is exact by construction: a test is skipped only if it is proven that nothing it executes has changed.

- **Nodes.** IR functions (commands, event application, queries, rules, methods, `check`, `on` and ACL reactions, fake implementations, the test itself), schema types (fields, indexes, method signatures and access) and event reaction tables. A node has a hash of its canonical form without source positions: comments, formatting and shifted test lines do not affect it.
- **Reachability.** From a test — by the symbols of instructions: the commands and queries of steps, calls, events, their application and reactions (in other bounded contexts too), rules, methods, projections, external requests. A timer command is reachable if time passes in the test (`time passes`); a request implementation in a fake — if the test calls the fake's commands (`Setup`).
- A test is **affected** if it is not in the base version or one of its nodes has changed. On `--driver web`, a change of the interface program (apps, screens, places, texts) affects every test.
- **A full run** — if something the analysis does not see has changed: any project file outside `*.rr` and `api/` (`reasonr.yaml`, `reasonr.lock`, `engine.yaml`, UI primitives), a new extension, or the base version does not build.
- **The base** is the common ancestor of `HEAD` and `--base` in git; the compiler builds it from git without a checkout. The nightly run of main is full.

`reasonr impact [--base ref] [--format json]` prints the affected tests of each model and the changed nodes through which a test is affected; `reasonr build --dump-impact` prints the graph: node hashes and the nodes of each test. `reasonrd test --impact <file>` takes the `reasonr impact --format json` report: the selection is computed once and shared by the CI runs.

**Feature flags — old tests do not change** (§23.4). If a change adds a flag and a test that does not switch the flag on has changed, `reasonr impact` fails (code 1): new behavior is covered by a test with a `feature` line. The exceptions are a commit that removes the flag (`reasonr feature remove`) and an explicit `--allow-test-changes "<test>"` with the reason in the commit message.

```text
$ reasonr impact --base origin/main examples/bank/src
base: running 5 of 10 tests
  A transfer beyond the overdraft is rejected as a whole — func command Accounts.Account.SetOverdraft
  A transfer within the antifraud limit does not freeze the account — func command Accounts.Account.SetOverdraft
  A transfer within the overdraft — func command Accounts.Account.SetOverdraft
  An error of the target cancels the transfer — func command Accounts.Account.SetOverdraft
  Antifraud freezes the account after a large transfer — func command Accounts.Account.SetOverdraft
```

Here the check in `SetOverdraft` has changed: the tests that set no overdraft (joining, forgetting data, another client's account) are skipped.

---

### 27.5. Demo scenarios

A demo scenario fills a tenant with plausible data **only with public commands**, like a test: the data is consistent — events, projections, uniqueness, personal data, timers. A scenario is a `test` in a `*.demo.rr` file with a single `given` section (`when` and `then` — E0805); loops, `faker` and `time offset` work as in tests. `reasonrd test` runs demos in memory together with the tests: a step error is a failure.

```rr
# taxi/contexts/Ordering/moscow.demo.rr
# Demo data (RFC §27.5): a week of taxi rides in Moscow — orders in every
# status. The scenario calls the matching and payment answers itself with
# their roles.
# reasonrd demo load "A week of rides in Moscow" --tenant demo --config engine.yaml

let moscow = GeoPoint(lat: 55.75, lon: 37.62)

test "A week of rides in Moscow"
  given
    time offset -7d
    for day in range(7)
      for k in range(12)
        let c = new Customer.Id
        let o = new Order.Id
        [c].SignUp(phoneNumber: faker.phone()) as c
        [c].OrderTaxi(
          orderId: o,
          orderRoute: Route(from: moscow, to: moscow, distanceKm: faker.float(1, 30)),
          requestedClass: economy,
        ) as c
        if faker.chance(0.75)
          let d = new Driver.Id
          [d].Register(
            driverName: faker.name(),
            driverCar: Car(
              plate: faker.pattern("?###??77"),
              model: faker.pick(["Kia Rio", "Lada Vesta", "Hyundai Solaris"]),
              carClass: economy,
            ),
          ) as admin
          [d].GoOnline() as d
          [Trip.Id(o)].DriverFound(driverId: d) as matching
          [d].Accept() as d
          if faker.chance(0.8)
            [d].FinishRide() as d
            [o].PaymentCaptured(transactionId: faker.pattern("tx-########")) as payments
        else if faker.chance(0.5)
          [o].Cancel(reason: faker.pick(["changed my mind", "the wait is too long", "plans changed"])) as c
        time offset +40m
      time offset +16h
```

The bank demo is a month of transfers between accounts with an overdraft:

```rr
# bank/contexts/Accounts/bank.demo.rr
# Demo data (RFC §27.5): a month of transfers — clients, accounts with an
# overdraft, transfers between them.
# reasonrd demo load "A month of transfers" --tenant demo --config engine.yaml

test "A month of transfers"
  given
    time offset -30d
    for i in range(10)
      let c = new Client.Id
      let main = new Account.Id
      let savings = new Account.Id
      [c].Join(clientName: faker.name()) as c
      [main].Open() as c
      [savings].Open() as c
      [main].SetOverdraft(limit: faker.decimal(3000, 5000)) as compliance
      for day in range(faker.int(3, 6))
        [main].Transfer(target: savings, sum: faker.decimal(10, 500), comment: faker.text(3)) as c
        time offset +1d
```

**The offset belongs to the scenario, not to the tenant.** The runner passes it in every request with the header `Reasonr-Time-Offset: -7d`: for the unit of work of that request, `now` = the current time + the offset. A timer remembers the offset of the unit that set it and is stored by the real moment: a 60-second timer set "a week ago" fires after 60 real seconds. Moving forward is `POST /_test/{tenant}/clock` with the same header: the engine at once runs the tenant's timers whose moment has come by the scenario clock. Two scenarios in one tenant each live in their own time. The header is accepted by a test mode server and by tenants with `demo: true` in `engine.yaml`; others answer 400: time is not shifted in production.

```yaml
tenants:
  demo: { demo: true }      # demo data: scenario clock offset and the test sign-in Authorization: Test …
```

```sh
reasonrd demo list examples/taxi/src
reasonrd demo load "A week of rides in Moscow" --tenant demo --config engine.yaml --repeat 4 --parallel 4 examples/taxi/src
```

`demo load` with `--config` starts the engine from the configuration in process, with `--url` it goes to a running server; `--repeat` is the number of scenario runs (run i uses `--seed`+i), `--parallel` — how many at once. The scenario calls the answers of external systems itself with their roles (`[Trip.Id(o)].DriverFound(…) as matching`).

### 27.6. Performance tests

A performance test is a `test` in a `*.perf.rr` file with the same sections as an acceptance test. `given` fills the system once — steps, loops, `faker`. `when` is the path of one user of several steps (commands, queries, `let`): the runner executes it with many virtual users, each with their own `let`, `new` and `faker`. `then` holds boolean expressions over the run statistics `stats` and the baseline run `baseline`:

| Field | What |
|---|---|
| `p50`, `p90`, `p95`, `p99`, `max` | API call latencies (`duration`) |
| `rate` | Completed paths per second (`float`) |
| `calls`, `errors`, `rejected` | Calls; faults (5xx, a broken connection); business errors |
| `of(Type.method)` | The same for one public command or query: `stats.of(Order.Card).p99` |

A check with `baseline` is skipped without a baseline run. A 10 % regression is `stats.p95 * 10 <= baseline.p95 * 11`: a duration is multiplied by an integer.

```rr
# taxi/contexts/Ordering/peak.perf.rr
# A performance test (RFC §27.6): the peak hour — customers sign up, order a
# taxi and look at the order card while drivers are online.
# reasonrd perf load --rate 300/s --config engine.yaml

let peakPoint = GeoPoint(lat: 55.75, lon: 37.62)
let peakRide = Route(from: peakPoint, to: peakPoint, distanceKm: 5)

test "Peak hour"
  given
    for i in range(50)
      let d = new Driver.Id
      [d].Register(
        driverName: faker.name(),
        driverCar: Car(plate: faker.pattern("?###??77"), model: "Kia Rio", carClass: economy),
      ) as admin
      [d].GoOnline() as d
  when
    let c = new Customer.Id
    let o = new Order.Id
    [c].SignUp(phoneNumber: faker.phone()) as c
    [c].OrderTaxi(orderId: o, orderRoute: peakRide, requestedClass: economy) as c
    Order.Card(orderId: o) as c
  then
    stats.p95 < 100ms
    stats.of(Customer.OrderTaxi).p99 < 300ms
    stats.errors == 0
    stats.p95 * 10 <= baseline.p95 * 11
```

**The load profile is runner flags, not language**: one test runs in different modes. The load is an open model: paths start on a schedule at the given rate, so a slow server neither lowers the load nor hides latencies. The server is a test mode server on the `--config` adapters, as for `--driver api`, or `--url`; each test runs in its own ephemeral tenant.

| Mode | Profile | What |
|---|---|---|
| `reasonrd test --serve [--listen :7070]`, `reasonrd test-worker --coordinator address [--drivers …] [--slots n]` | Running tests with workers (§27.3): a job queue with leases at the coordinator, workers pull jobs themselves |
| `reasonrd perf smoke` | 20/s, 1 min | A quick check on every change |
| `reasonrd perf load --rate 300/s --duration 15m --ramp 2m` | ramp-up and the expected load | Whether the system holds the load |
| `reasonrd perf capacity [--rate 10/s] [--duration 30s]` | steps ×1.5 | The highest rate at which `then` holds and the generator keeps up |

The report is text or JSON (`--format json`); it is also the baseline: `--baseline report.json`. Exit code 1 means a `then` check is false. `reasonrd test` runs a performance test once (given and one path) and does not evaluate the `then` checks.
