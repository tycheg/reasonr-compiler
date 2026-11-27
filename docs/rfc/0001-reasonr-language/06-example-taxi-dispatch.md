### 25.4. Driver and matching: bounded context Dispatch

The driver, matching a driver for an order, offers and the trip are in bounded context `Dispatch`. Its types:

```rr
# taxi/contexts/Dispatch/types.rr
# Dispatch's own types: money, a point and a car class are copies of the
# Ordering types, translated by the ACL (ordering.acl.rr).

enum DriverStatus
  offline
  free
  offered
  busy

enum OfferOutcome
  pending
  accepted
  declined
  expired
  withdrawn

enum CarClass
  economy
  comfort

enum Currency
  rub

value Money
  amount decimal
  currency Currency

value GeoPoint
  lat float
  lon float

value Car
  plate string
  model string
  carClass CarClass

value DriverInfo
  driverId Driver.Id
  name string
  plate string

value DriverState
  status DriverStatus
  currentTrip Trip.Id?
```

```rr
# taxi/contexts/Dispatch/driver.rr

actor Driver
  id uuid
  name string
  car Car
  status DriverStatus              # offline by default
  currentTrip Trip.Id?

  index byStatus(status, car.carClass)
  index byPlate(car.plate) unique plateTaken

  command(admin) Register(driverName string, driverCar Car)
    if [id]
      alreadyRegistered()
      return
    driverRegistered(driverName: driverName, driverCar: driverCar)

  command(self) GoOnline()
    if not [id]
      notRegistered()
    if status != offline
      alreadyOnline()
    if hasErrors
      return
    wentOnline()

  command(self) GoOffline()
    if status != free
      cannotGoOffline(currentStatus: status)
      return
    wentOffline()

  command offerTrip(tripId Trip.Id, pickup GeoPoint, fare Money)
    if status != free
      offerRejectedBusy(tripId: tripId)        # a fact: the driver is busy, the offer is not taken
      [tripId].driverDeclined(driverId: id)    # the trip goes to the next driver
      return
    tripOffered(tripId: tripId)
    newOffer(tripId: tripId, pickup: pickup, fare: fare) to id

  ## Accept the offered order
  command(self) Accept()
    if status != offered or currentTrip is none
      noActiveOffer()
      return
    offerAccepted()
    [currentTrip].assignDriver(driverId: id, assignedName: name, assignedPlate: car.plate)  # an order error cancels offerAccepted too

  ## Decline the offered order
  command(self) Decline()
    if status != offered or currentTrip is none
      noActiveOffer()
      return
    offerDeclined()
    [currentTrip].driverDeclined(driverId: id)

  command withdrawOffer(tripId Trip.Id)
    if status != offered or currentTrip != tripId
      return
    offerWithdrawn()
    offerCancelled(tripId: tripId) to id

  ## Finish the ride
  command(self) FinishRide()
    if status != busy or currentTrip is none
      noActiveRide()
      return
    rideFinished()
    [currentTrip].finishRide(driverId: id)

  command releaseTrip(tripId Trip.Id)
    if currentTrip != tripId
      return
    rideFinished()

  event driverRegistered(driverName string, driverCar Car)
    name = driverName
    car = driverCar

  event wentOnline()
    status = free

  event wentOffline()
    status = offline

  event tripOffered(tripId Trip.Id)
    status = offered
    currentTrip = tripId

  event offerAccepted()
    status = busy

  event offerRejectedBusy(tripId Trip.Id)    # an empty event — a fact for audit and analytics

  event offerDeclined()
    status = free
    currentTrip = none

  event offerWithdrawn()
    status = free
    currentTrip = none

  event rideFinished()
    status = free
    currentTrip = none

  notification newOffer(tripId Trip.Id, pickup GeoPoint, fare Money)
  notification offerCancelled(tripId Trip.Id)

  error alreadyRegistered
  error notRegistered
  error alreadyOnline
  error cannotGoOffline(currentStatus DriverStatus)
  error noActiveOffer
  error noActiveRide
  error plateTaken
  error driverNotFound

  ## The current driver's state
  query(Driver) MyState() DriverState
    let d = [me]
    if d is none
      notRegistered()
      return
    return DriverState(status: d.status, currentTrip: d.currentTrip)

  ## Free drivers for the dispatcher
  query(dispatcher) FreeDrivers(wantedClass CarClass, pageReq PageRequest) page<DriverInfo>
    return [Driver]
      where status == free and car.carClass == wantedClass
      page pageReq
      select DriverInfo(driverId: id, name: name, plate: car.plate)

  query info(driverId Driver.Id) DriverInfo
    let d = [driverId]
    if d is none
      driverNotFound()
      return
    return DriverInfo(driverId: driverId, name: d.name, plate: d.car.plate)
```

The trip is a `Dispatch` aggregate with its own fields, events and types. The ACL opens it on an order event (`on Ordering.Order.placed`), and it has the same uuid as the order; `Ordering` learns about the matching and ride progress with its own ACL from the trip events (§25.3):

```rr
# taxi/contexts/Dispatch/trip.rr
# The trip: driver matching, offers, the ride. It opens for an Ordering order
# (ordering.acl.rr) and knows about it only what the ACL passed. Offers to
# drivers are the inner entity Trip.Offer (trip.offer.rr).

enum TripStage
  searching
  offered
  assigned
  completed
  noDrivers
  cancelled

aggregate Trip
  id uuid
  stage TripStage
  pickup GeoPoint
  carClass CarClass
  fare Money
  driver Driver.Id?
  candidate Driver.Id?
  searchTimer Timer.Id?
  offerTimer Timer.Id?
  searchRetries int

  # ---------- the order (ordering.acl.rr) ----------

  command open(pickupPoint GeoPoint, requestedClass CarClass, price Money)
    searchOpened(pickupPoint: pickupPoint, requestedClass: requestedClass, tripFare: price)
    findDriver()

  command update(pickupPoint GeoPoint, requestedClass CarClass, price Money)
    searchUpdated(pickupPoint: pickupPoint, requestedClass: requestedClass, tripFare: price)

  command close()
    delete searchTimer
    delete offerTimer
    closeOffer(result: withdrawn)
    [candidate]?.withdrawOffer(tripId: id)
    [driver]?.releaseTrip(tripId: id)
    rideCancelled(tripId: id) to driver
    dispatchClosed()

  command tip(amount Money)
    tipReceived(tripId: id, amount: amount) to driver

  # ---------- matching ----------

  ## Drivers already offered the trip are not matched again
  command findDriver()
    let offered set<Driver.Id> = []
    for o in [Offer]
      if o.driver is not none
        offered = offered.with(o.driver)
    let timer = schedule(at: now + 60s, run: searchTimedOut())
    Matching.findDriver(tripId: id, pickup: pickup, requestedClass: carClass, exclude: offered, timeout: timer)
    searchStarted(timer: timer)

  ## The matching service answers: a driver is found.
  ## The offer goes to the driver as the last step: if the driver is busy,
  ## they return the order at once (driverDeclined), and matching the next driver starts.
  command(matching) DriverFound(driverId Driver.Id)
    if not [id] or stage != searching
      return                                   # no trip or it is closed: a stale answer
    delete searchTimer
    let timer = schedule(at: now + 20s, run: expireOffer(driverId: driverId))
    [new Offer.Id].open(offeredTo: driverId)
    offerSent(driverId: driverId, timer: timer)
    [driverId].offerTrip(tripId: id, pickup: pickup, fare: fare)

  ## The matching service answers: no drivers
  command(matching) NoDriverFound()
    if not [id] or stage != searching
      return
    delete searchTimer
    giveUp()

  ## The search gave no result in time: start over, give up after three attempts
  command searchTimedOut()
    if stage != searching
      return
    if searchRetries >= 3
      giveUp()
      return
    searchRetried()
    findDriver()

  command giveUp()
    searchFailed(at: now)

  command searchAgain(skipped Driver.Id)
    driverSkipped(driverId: skipped)
    findDriver()

  command expireOffer(driverId Driver.Id)
    if stage != offered or candidate != driverId
      return
    closeOffer(result: expired)
    [driverId].withdrawOffer(tripId: id)
    searchAgain(skipped: driverId)

  ## The driver declined the offer or was busy
  command driverDeclined(driverId Driver.Id)
    if stage != offered or candidate != driverId
      return
    delete offerTimer
    closeOffer(result: declined)
    searchAgain(skipped: driverId)

  ## The driver's answer to the open offer
  command closeOffer(result OfferOutcome)
    for o in [Offer]
      if o.outcome == pending
        [o.id].close(result: result)

  # ---------- the ride ----------

  command assignDriver(driverId Driver.Id, assignedName string, assignedPlate string)
    if stage != offered or candidate != driverId
      offerNoLongerValid()
      return
    delete offerTimer
    closeOffer(result: accepted)
    driverAssigned(driverId: driverId, assignedName: assignedName, assignedPlate: assignedPlate)

  command finishRide(driverId Driver.Id)
    if stage != assigned or driver != driverId
      rideNotActive()
      return
    rideCompleted(at: now)

  # ---------- events ----------

  event searchOpened(pickupPoint GeoPoint, requestedClass CarClass, tripFare Money)
    pickup = pickupPoint
    carClass = requestedClass
    fare = tripFare
    stage = searching

  event searchUpdated(pickupPoint GeoPoint, requestedClass CarClass, tripFare Money)
    pickup = pickupPoint
    carClass = requestedClass
    fare = tripFare

  event searchStarted(timer Timer.Id)
    searchTimer = timer

  event searchRetried()
    searchRetries = searchRetries + 1

  event offerSent(driverId Driver.Id, timer Timer.Id)
    candidate = driverId
    offerTimer = timer
    searchTimer = none
    stage = offered

  event driverSkipped(driverId Driver.Id)
    candidate = none
    offerTimer = none
    stage = searching

  event driverAssigned(driverId Driver.Id, assignedName string, assignedPlate string)
    driver = driverId
    candidate = none
    offerTimer = none
    stage = assigned

  event rideCompleted(at timestamp)
    stage = completed

  event searchFailed(at timestamp)
    stage = noDrivers
    searchTimer = none

  event dispatchClosed()
    stage = cancelled
    searchTimer = none
    offerTimer = none

  notification rideCancelled(tripId Trip.Id)
  notification tipReceived(tripId Trip.Id, amount Money)

  error offerNoLongerValid
  error rideNotActive
```

Offers to drivers are an inner entity of the trip (§10.7): their own identity inside the trip, stored in its document:

```rr
# taxi/contexts/Dispatch/trip.offer.rr

## An offer of the trip to a driver: its own identity inside the trip
entity Trip.Offer
  id sequence                      # offer number within the trip: 1, 2, 3…
  driver Driver.Id?
  outcome OfferOutcome             # pending until the driver answers

  command open(offeredTo Driver.Id)
    opened(offeredTo: offeredTo)

  command close(result OfferOutcome)
    if outcome != pending
      return
    closed(result: result)

  event opened(offeredTo Driver.Id)
    driver = offeredTo

  event closed(result OfferOutcome)
    outcome = result
```

The `Dispatch` ACL opens the trip on an order event: it translates money, the point and the class into `Dispatch` types (§23.2):

```rr
# taxi/contexts/Dispatch/ordering.acl.rr
# Ordering orders in Dispatch's terms: a trip with the same uuid as the
# order, its own money, point and class.

acl Ordering
  ## An order is placed: open a trip and start matching a driver
  on Ordering.Order.placed
    [Trip.Id(id)].open(
      pickupPoint: GeoPoint(lat: orderRoute.from.lat, lon: orderRoute.from.lon),
      requestedClass: CarClass(requestedClass),
      price: Money(amount: orderPrice.amount, currency: Currency(orderPrice.currency)),
    )

  ## The route and class changed while no driver was found
  on Ordering.Order.edited
    [Trip.Id(id)].update(
      pickupPoint: GeoPoint(lat: orderRoute.from.lat, lon: orderRoute.from.lon),
      requestedClass: CarClass(requestedClass),
      price: Money(amount: orderPrice.amount, currency: Currency(orderPrice.currency)),
    )

  ## The order is cancelled: withdraw the offer and release the driver
  on Ordering.Order.orderCancelled
    [Trip.Id(id)].close()

  ## The customer left a tip: tell the driver
  on Ordering.Order.tipped
    [Trip.Id(id)].tip(amount: Money(amount: amount.amount, currency: Currency(amount.currency)))
```

The matching service is an external system of this bounded context:

```rr
# taxi/contexts/Dispatch/matching.rr

external(matching) Matching
  request findDriver(tripId Trip.Id, pickup GeoPoint, requestedClass CarClass, exclude set<Driver.Id>, timeout Timer.Id)
```

### 25.5. Projection, external systems, screens

```rr
# taxi/contexts/Ordering/board.rr

## The dispatcher board: orders together with driver data
projection OrderMart
  id Order.Id                      # own type OrderMart.Id based on Order.Id
  status OrderStatus
  driverName string?
  plate string?
  placedAt timestamp

  index byStatus(status, placedAt)

  query(dispatcher) Board(wanted OrderStatus, pageReq PageRequest) page<BoardRow>
    return [OrderMart]
      where status == wanted
      sort placedAt
      page pageReq
      select BoardRow(orderId: Order.Id(id), status: status, driverName: driverName, plate: plate)

external(payments) Payments
  request charge(paymentId Order.Id, amount Money)
```

Screens live in the bounded contexts of their data: the passenger's and the dispatcher board in `Ordering`, the driver's and the free drivers in `Dispatch`. Each app is in its own directory `apps/<App>/` (§23) and assembles screens of both bounded contexts:

```rr
# taxi/contexts/Ordering/ui.rr
# Passenger screens (apps/Taxi) and the dispatcher board (apps/Dispatch); admin screens are in admin.rr (§22.3)

screen NewOrder "Order a taxi"
  Form Customer.OrderTaxi(orderId: new)          # the target is me; route and class are form fields
    on done MyOrder

screen MyOrders "My orders"
  List Order.MyOrders                            # pageReq is pagination
    status
    price
    placedAt
    on open MyOrder

screen MyOrder(orderId Order.Id) "Order"
  Card Order.Card                                # fields are automatic
    Actions
      Confirm Order.Cancel if status in [searching, offered, assigned]
      Edit Order.LeaveTip "Tip" if tips and status == paid

screen Profile "Profile"
  Edit Customer.SignUp "Sign up"
  Card Customer.Profile
    Actions
      Confirm Customer.Forget

screen JoinTaxi "Become a customer"
  Form Customer.Join                             # the target is me: an Auth user acts as a customer

screen TaxiAccount "Taxi customer"
  Card Customer.Profile

screen Home "Home"
  Card Customer.Home
    Actions
      Edit Customer.SetHome "Change address"

screen Board "Order board"
  Table OrderMart.Board                          # wanted is a filter, pageReq is pagination
```

```rr
# taxi/contexts/Dispatch/ui.rr
# Driver screens (apps/DriverApp), free drivers for the dispatcher (apps/Dispatch)
# and a new driver for the admin app (apps/Admin)

screen Shift "Shift"
  Card Driver.MyState
    Actions
      Button Driver.GoOnline if status == offline
      Button Driver.GoOffline if status == free
      Button Driver.Accept if status == offered
      Button Driver.Decline if status == offered
      Button Driver.FinishRide if status == busy

screen FreeDrivers "Free drivers"
  Table Driver.FreeDrivers

screen NewDriver "New driver"
  Form [new Driver.Id].Register
```

Dispatch texts:

```rr
# taxi/contexts/Dispatch/texts.en.rr
# Dispatch texts in the apps' base language

texts en
  Trip.tipReceived "Tip {amount}"
```

```rr
# taxi/apps/Taxi/app.rr

app(Customer) Taxi "Taxi"
  language en
  platforms web, mobile
  Menu
    NewOrder
    MyOrders
    Profile
```

```rr
# taxi/apps/DriverApp/app.rr

app(Driver) DriverApp "Driver"
  language en
  platforms mobile
  Menu
    Shift
```

```rr
# taxi/apps/Dispatch/app.rr

app(dispatcher) Dispatch "Dispatch"
  language en
  Menu
    Board
    FreeDrivers
```

### 25.6. What to notice

- **The order and the trip are different aggregates of different bounded contexts.** `Ordering.Order` and `Dispatch.Trip` are connected only by ACLs: `Dispatch` does not read the order status or call its commands — it learns about placing, changes, cancellation and tips from order events (`on Ordering.Order.orderCancelled` in `ordering.acl.rr`), and `Ordering` learns about matching and the ride from trip events (`dispatch.acl.rr`). Each bounded context declares its own money, point and car class; the ACL translates them with constructors and `CarClass(requestedClass)`.
- **ACL reactions come after the command.** ACL reactions run after the root command in event order, in the same unit of work: the order status goes through the same steps as the trip matching.
- **Offers are an inner entity of the trip.** `Trip.Offer` keeps whom each offer went to and how it ended; drivers of earlier offers are not matched again (`for o in [Offer]`). It is stored in the trip document.
- **A busy driver is not an error.** If the driver is already busy, `offerTrip` raises `offerRejectedBusy` and returns the trip with the `driverDeclined` command. The offer closes, and the trip goes straight to matching the next driver (`searchAgain`). The call to the driver comes last in `DriverFound`: the reentrant `driverDeclined` sees the offer already sent and cancels its timer.
- **A safety net.** If the matching service does not answer, after 60 seconds `searchTimedOut` starts the search again (up to three times).
- **Atomicity across aggregates and bounded contexts.** `Driver.Accept` raises `offerAccepted` and calls `Trip.assignDriver`, and the `Ordering` ACL assigns the driver to the customer's order. If the trip is already closed, no effect is applied.
- **`?.` instead of checks.** `[candidate]?.withdrawOffer(...)` and `[driver]?.releaseTrip(...)` do nothing if there is no driver.
- **Existence through `[id]`.** No "registered" flags are needed.
- **Uniqueness.** The customer phone and the car plate are declared unique in the model; the engine chooses the mechanism.
- **Personal data.** The customer phone is encrypted with the customer's key; `Forget` destroys the key.
- **An extension point.** `perKmRate` is a rule an extension may override.
