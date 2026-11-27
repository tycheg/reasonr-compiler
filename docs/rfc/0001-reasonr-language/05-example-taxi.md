## 25. Example 1: taxi

A customer orders a taxi, an external service matches a driver, the driver gets an offer with a timeout and accepts or declines it, and after the ride the payment goes through. There are queries, a projection for the dispatcher, screens, uniqueness and personal data.

The taxi is two bounded contexts (§23):

- **`Ordering`** — the customer, staff and the order: price, status, cancellation, payment, the dispatcher board;
- **`Dispatch`** — the driver and the trip: driver matching, offers, the ride.

Each bounded context has its own model and its own types (money, a point, a car class). They are connected only by ACLs (§23.2): on `on Ordering.Order.placed`, `Dispatch` opens a trip with the same uuid as the order and translates its data into its own types; on `on Dispatch.Trip.driverAssigned` and other trip events, `Ordering` keeps the order status. A Keycloak user (module `Auth`) is a customer with the same identifier: `actor Auth.User as Customer`.

### 25.1. Types and roles

```rr
# taxi/contexts/Ordering/types.rr

role admin
role support
role dispatcher
role matching
role payments

## Tips for the driver after a paid ride
feature tips "2026-11-07"

enum OrderStatus
  searching
  offered
  assigned
  completed
  paid
  unpaid
  cancelled
  noDrivers

enum CarClass
  economy
  comfort

enum Currency
  rub

value Money
  amount decimal
  currency Currency
  check amount >= 0

value GeoPoint
  lat float
  lon float
  check lat >= -90 and lat <= 90
  check lon >= -180 and lon <= 180

value Route
  from GeoPoint
  to GeoPoint
  distanceKm float

value OrderRow
  orderId Order.Id
  status OrderStatus
  price Money
  placedAt timestamp

value OrderCard
  status OrderStatus
  price Money
  driver AssignedDriver?
  tip Money?

value AssignedDriver
  name string
  plate string

value BoardRow
  orderId Order.Id
  status OrderStatus
  driverName string?
  plate string?

value CustomerProfile
  phone string?
  blocked bool

value HomeAddress
  address string?

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

### 25.2. Customer

```rr
# taxi/contexts/Ordering/customer.rr

actor Customer
  id uuid
  phone string? personal
  blocked bool
  activeOrder Order.Id?
  homeAddress string?

  index byPhone(phone) unique phoneTaken

  ## Sign up
  command(self) SignUp(phoneNumber string personal)
    if [id]
      alreadyRegistered()
      return
    signedUp(phoneNumber: phoneNumber)

  ## Become a customer through a Keycloak account: an Auth user is a
  ## customer with the same identifier (auth.acl.rr)
  command(self) Join(phoneNumber string personal)
    if [id]
      alreadyRegistered()
      return
    joined(phoneNumber: phoneNumber)

  ## Home address, filled into new orders
  command(self) SetHome(address string)
    homeSet(address: address)

  ## Order a taxi. The client generates the order identifier; staff may
  ## also place an order for a customer.
  command(self, admin, support) OrderTaxi(orderId Order.Id, orderRoute Route, requestedClass CarClass)
    if not [id]
      notRegistered()
    if blocked
      customerBlocked()
    if activeOrder is not none
      alreadyHasOrder(orderId: activeOrder)
    if hasErrors
      return
    orderStarted(orderId: orderId)
    [orderId].place(customerId: id, orderRoute: orderRoute, requestedClass: requestedClass)

  ## Delete personal data
  command(self, support) Forget()
    if activeOrder is not none
      hasActiveOrder()
      return
    forget id
    forgotten()

  command orderFinished(orderId Order.Id)
    if activeOrder == orderId
      orderClosed()

  command orderUnpaid(orderId Order.Id)
    blockedForDebt(orderId: orderId)

  event signedUp(phoneNumber string? personal)
    phone = phoneNumber

  event joined(phoneNumber string? personal)
    phone = phoneNumber

  event homeSet(address string)
    homeAddress = address

  event orderStarted(orderId Order.Id)
    activeOrder = orderId

  event orderClosed()
    activeOrder = none

  event blockedForDebt(orderId Order.Id)
    blocked = true
    activeOrder = none

  event forgotten()

  error alreadyRegistered
  error notRegistered
  error customerBlocked
  error alreadyHasOrder(orderId Order.Id)
  error hasActiveOrder
  error phoneTaken

  query(Customer) Profile() CustomerProfile
    let c = [me]
    if c is none
      notRegistered()
      return
    return CustomerProfile(phone: c.phone, blocked: c.blocked)

  query(Customer) Home() HomeAddress
    let c = [me]
    if c is none
      return HomeAddress(address: none)
    return HomeAddress(address: c.homeAddress)

  ## Find a customer by phone
  query(admin, support) FindByPhone(phoneNumber string personal) list<CustomerRow>
    return [Customer]
      where phone == phoneNumber
      limit 10
      select CustomerRow(customerId: id, phone: phone)
```

### 25.3. Order: bounded context Ordering

```rr
# taxi/contexts/Ordering/order.rr
# The order as the customer and staff see it: placing, price, status,
# cancellation, payment. Driver matching and the ride are run by the trip of
# bounded context Dispatch: its progress comes through the ACL
# (dispatch.acl.rr).

aggregate Order
  id uuid
  status OrderStatus
  customer Customer.Id?
  route Route
  carClass CarClass
  price Money
  driverName string?
  driverPlate string?
  placedAt timestamp
  tip Money?

  index byCustomer(customer, placedAt desc)
  index byStatus(status, placedAt desc)

  # ---------- rules ----------

  ## Rate per kilometer. An extension may override it.
  rule perKmRate(requestedClass CarClass) decimal
    if requestedClass == comfort
      return 45
    return 30

  # ---------- commands ----------

  ## The customer places the order (Customer.OrderTaxi); Dispatch starts
  ## matching a driver on the placed event (its ACL is Dispatch/ordering.acl.rr)
  command place(customerId Customer.Id, orderRoute Route, requestedClass CarClass)
    if [id]
      orderAlreadyExists()
      return
    let perKm = perKmRate(requestedClass: requestedClass)
    let orderPrice = Money(amount: 100 + perKm * decimal(orderRoute.distanceKm), currency: rub)
    placed(
      customerId: customerId,
      orderRoute: orderRoute,
      requestedClass: requestedClass,
      orderPrice: orderPrice,
      at: now,
    )
    writeMart()
    orderPlaced(orderId: id, price: orderPrice)

  ## The payment service answers: the money is captured
  command(payments) PaymentCaptured(transactionId string)
    if status != completed
      return
    paymentReceived(transactionId: transactionId)
    writeMart()
    [customer]?.orderFinished(orderId: id)
    receipt(orderId: id, amount: price) to customer

  ## The payment service answers: declined
  command(payments) PaymentDeclined(reason string)
    if status != completed
      return
    paymentFailed(reason: reason)
    writeMart()
    [customer]?.orderUnpaid(orderId: id)
    paymentProblem(orderId: id, reason: reason) to customer

  ## Tips for the driver after a paid ride, under flag tips
  command(customer) LeaveTip(amount Money)
    if tips
      if status != paid
        cannotTip(currentStatus: status)
        return
      tipped(amount: amount)                   # Dispatch tells the driver
    else
      tipsDisabled()

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

  ## Cancelling an order by the customer, support or an administrator;
  ## Dispatch releases the offer and the driver on the orderCancelled event
  command(customer, support, admin) Cancel(reason string)
    if status not in [searching, offered, assigned]
      cannotCancel(currentStatus: status)
      return
    orderCancelled(reason: reason, at: now)
    writeMart()
    [customer]?.orderFinished(orderId: id)

  ## The dispatcher board row is written whole, from the order fields
  command writeMart()
    OrderMart(id: OrderMart.Id(id), status: status, driverName: driverName, plate: driverPlate, placedAt: placedAt)

  # ---------- matching and ride progress (dispatch.acl.rr) ----------

  ## An offer is sent to a driver
  command markOffered()
    offerPending()
    writeMart()

  ## The driver declined or did not answer; matching goes on
  command resumeSearch()
    searchResumed()
    writeMart()

  command confirmDriver(assignedName string, assignedPlate string)
    driverConfirmed(assignedName: assignedName, assignedPlate: assignedPlate)
    writeMart()
    driverOnTheWay(orderId: id) to customer

  command failSearch()
    searchExhausted()
    writeMart()
    [customer]?.orderFinished(orderId: id)
    noDriversAvailable(orderId: id) to customer

  ## The ride is over: charge the payment
  command finishRide()
    rideFinished()
    writeMart()
    Payments.charge(paymentId: id, amount: price)

  # ---------- events ----------

  event placed(customerId Customer.Id, orderRoute Route, requestedClass CarClass, orderPrice Money, at timestamp)
    customer = customerId
    route = orderRoute
    carClass = requestedClass
    price = orderPrice
    placedAt = at
    status = searching

  event offerPending()
    status = offered

  event searchResumed()
    status = searching

  event driverConfirmed(assignedName string, assignedPlate string)
    driverName = assignedName
    driverPlate = assignedPlate
    status = assigned

  event searchExhausted()
    status = noDrivers

  event rideFinished()
    status = completed

  event paymentReceived(transactionId string)
    status = paid

  event paymentFailed(reason string)
    status = unpaid

  event tipped(amount Money)
    tip = amount

  event edited(orderRoute Route, requestedClass CarClass, orderPrice Money)
    route = orderRoute
    carClass = requestedClass
    price = orderPrice

  event orderCancelled(reason string, at timestamp)
    status = cancelled

  # ---------- notifications and errors ----------

  notification orderPlaced(orderId Order.Id, price Money)
  notification driverOnTheWay(orderId Order.Id)
  notification noDriversAvailable(orderId Order.Id)
  notification receipt(orderId Order.Id, amount Money)
  notification paymentProblem(orderId Order.Id, reason string)

  error orderAlreadyExists
  error cannotCancel(currentStatus OrderStatus)
  error cannotEdit(currentStatus OrderStatus)
  error orderNotFound
  error tipsDisabled
  error cannotTip(currentStatus OrderStatus)

  # ---------- queries ----------

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
      orderNotFound()
      return
    let driverInfo AssignedDriver? = none
    if o.driverName is not none
      driverInfo = AssignedDriver(name: o.driverName, plate: o.driverPlate ?? "")
    let tipAmount Money? = none
    if tips
      tipAmount = o.tip
    return OrderCard(status: o.status, price: o.price, driver: driverInfo, tip: tipAmount)

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
```

The matching and ride progress comes from `Dispatch` through the order's ACL (§23.2):

```rr
# taxi/contexts/Ordering/dispatch.acl.rr
# Dispatch matching and ride progress in the order's terms. The trip has the
# same uuid as the order.

acl Dispatch
  ## An offer is sent to a driver
  on Dispatch.Trip.offerSent
    [Order.Id(id)].markOffered()

  ## The driver declined or did not answer; matching goes on
  on Dispatch.Trip.driverSkipped
    [Order.Id(id)].resumeSearch()

  on Dispatch.Trip.driverAssigned
    [Order.Id(id)].confirmDriver(assignedName: assignedName, assignedPlate: assignedPlate)

  on Dispatch.Trip.searchFailed
    [Order.Id(id)].failSearch()

  ## The ride is over: charge the payment
  on Dispatch.Trip.rideCompleted
    [Order.Id(id)].finishRide()
```
