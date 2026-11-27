# W — warnings

## W1001

A private command, event, error, notification, rule or field is never used. It is most likely a refactoring leftover or a forgotten call; unused declarations make the model harder to read.

```rr
role admin

aggregate Order
  id uuid

  command(admin) Close()
    return

  command archive()
    return
```

```rr
role admin

aggregate Order
  id uuid

  command(admin) Close()
    archive()

  command archive()
    return
```

## W1002

Code after return, break, continue or a branch that ends on every path never runs.

```rr
role admin

aggregate Order
  id uuid
  status int

  command(admin) Close()
    return
    closed()

  event closed()
    status = 2
```

```rr
role admin

aggregate Order
  id uuid
  status int

  command(admin) Close()
    closed()

  event closed()
    status = 2
```

## W1003

A while loop changes none of its condition variables and has no break or return: if the condition holds, the loop never ends and the engine aborts the unit of work at the iteration limit.

```rr
role admin

aggregate Counter
  id uuid
  total int

  command(admin) Count()
    let n = 0
    while n < 10
      ticked()

  event ticked()
    total = total + 1
```

```rr
role admin

aggregate Counter
  id uuid
  total int

  command(admin) Count()
    let n = 0
    while n < 10
      ticked()
      n = n + 1

  event ticked()
    total = total + 1
```

## W1004

A command is called in a loop over an unbounded collection: each iteration loads an entity, and the unit of work may grow huge. Bound the collection or spread the work over timers.

```rr
role admin

aggregate Order
  id uuid
  items list<Item.Id>

  command(admin) Close()
    for i in items
      [i].close()

aggregate Item
  id uuid

  command close()
    return
```

```rr
role admin

aggregate Order
  id uuid
  first Item.Id?

  command(admin) Close()
    [first]?.close()

aggregate Item
  id uuid

  command close()
    return
```

## W1005

A field or parameter with a money-like name (price, amount, sum, balance…) is a float: binary fractions lose cents. Use decimal for money.

```rr
value Money
  amount float
```

```rr
value Money
  amount decimal
```

## W1006

The command is only called by a timer, so it has no human initiator, while a notification without to goes to the root command's initiator. Name the recipient with to.

```rr
role admin

actor Customer
  id uuid

aggregate Order
  id uuid
  customer Customer.Id?
  timer Timer.Id?

  command(admin) Start()
    let t = schedule(at: now + 1h, run: remind())
    started(t: t)

  command remind()
    reminder()

  event started(t Timer.Id)
    timer = t

  notification reminder()
```

```rr
role admin

actor Customer
  id uuid

aggregate Order
  id uuid
  customer Customer.Id?
  timer Timer.Id?

  command(admin) Start()
    let t = schedule(at: now + 1h, run: remind())
    started(t: t)

  command remind()
    reminder() to customer

  event started(t Timer.Id)
    timer = t

  notification reminder()
```

## W1010

A public command has no access list, so any authenticated user can call it. If that is intended, keep the warning; usually access is given in parentheses: command(Customer) …, command(admin) ….

```rr
aggregate Order
  id uuid

  command Close()
    return
```

```rr
role admin

aggregate Order
  id uuid

  command(admin) Close()
    return
```

## W1030

An external system answers by calling a public command with its role (RFC §18). If no public command of the model is available to that role, the system's answer always gets forbidden. Add the role to the access list of the answer command.

```rr
role payments

external(payments) Payments
  request charge(paymentId Order.Id)

aggregate Order
  id uuid

  command finish()
    Payments.charge(paymentId: id)
    finished()

  event finished()
```

```rr
role payments

external(payments) Payments
  request charge(paymentId Order.Id)

aggregate Order
  id uuid

  command finish()
    Payments.charge(paymentId: id)
    finished()

  command(payments) PaymentCaptured()
    finished()

  event finished()
```

## W1020

forget destroys a subject's personal data keys, but this entity has no personal data — forget erases nothing. Check that this is the right subject and that its fields are marked personal.

```rr
actor Customer
  id uuid
  phone string?

  command(self) Forget()
    forget id
```

```rr
actor Customer
  id uuid
  phone string? personal

  command(self) Forget()
    forget id
```

## W1021

A non-personal command parameter flows into a personal place. The value is encrypted in storage, but as a command argument it stays in engine logs. Mark the parameter personal.

```rr
actor Customer
  id uuid
  phone string? personal

  command(self) SignUp(phoneNumber string)
    signedUp(phoneNumber: phoneNumber)

  event signedUp(phoneNumber string? personal)
    phone = phoneNumber
```

```rr
actor Customer
  id uuid
  phone string? personal

  command(self) SignUp(phoneNumber string personal)
    signedUp(phoneNumber: phoneNumber)

  event signedUp(phoneNumber string? personal)
    phone = phoneNumber
```

## W1101

A command was removed from the model. If already scheduled timers refer to it, they will have nothing to run: a migration must delete or redirect them. The engine checks the data for such timers.

```rr
# file: old/m.rr
role admin

aggregate Order
  id uuid

  command(admin) Close()
    return

  command(admin) Archive()
    return

# file: new/m.rr
role admin

aggregate Order
  id uuid

  command(admin) Close()
    return
```

```rr
# file: old/m.rr
role admin

aggregate Order
  id uuid

  command(admin) Close()
    return

  command(admin) Archive()
    return

# file: new/m.rr
role admin

aggregate Order
  id uuid

  command(admin) Close()
    return

  command(admin) Archive()
    return
```

## W1102

A field or entity was removed from the model: its data is lost during migration. If the field was renamed, give the old name with was — the data then moves over.

```rr
# file: old/m.rr
aggregate Order
  id uuid
  note string

# file: new/m.rr
aggregate Order
  id uuid
  comment string
```

```rr
# file: old/m.rr
aggregate Order
  id uuid
  note string

# file: new/m.rr
aggregate Order
  id uuid
  comment string was note
```

## W1103

A unique index was added. During migration the engine checks existing data for duplicates and stops with a list of violations if it finds any: fix the data beforehand.

```rr
# file: old/m.rr
actor Customer
  id uuid
  phone string

  index byPhone(phone)

# file: new/m.rr
actor Customer
  id uuid
  phone string

  index byPhone(phone) unique phoneTaken

  error phoneTaken(phone string)
```

```rr
# file: old/m.rr
actor Customer
  id uuid
  phone string

  index byPhone(phone) unique phoneTaken

  error phoneTaken(phone string)

# file: new/m.rr
actor Customer
  id uuid
  phone string

  index byPhone(phone) unique phoneTaken

  error phoneTaken(phone string)
```

## W1301

The node is bound to a method nobody who enters the app can call: access comes from the model, and no user will ever see such a node. Either the node is in the wrong app or the method has the wrong access.

```rr
value DriverView
  online bool

actor Customer
  id uuid

actor Driver
  id uuid

  query(Driver) State() DriverView
    return DriverView(online: true)

app(Customer) Shop
  Menu
    Home

screen Home
  Card Driver.State
```

```rr
value DriverView
  online bool

actor Customer
  id uuid

actor Driver
  id uuid

  query(Driver) State() DriverView
    return DriverView(online: true)

app(Driver) Cab
  Menu
    Home

screen Home
  Card Driver.State
```

## W1302

The screen belongs to no app: it is not in a menu, no Link or on transition leads to it, it is not embedded anywhere. Most likely it was not added to a menu or is a refactoring leftover.

```rr
actor Customer
  id uuid

app(Customer) Shop
  Menu
    Home

screen Home
  Toolbar

screen Help
  Toolbar
```

```rr
actor Customer
  id uuid

app(Customer) Shop
  Menu
    Home
    Help

screen Home
  Toolbar

screen Help
  Toolbar
```

## W1303

The model has texts in another language, but this text of the app's base language is missing there: a user in that language will see the name split into words or the base-language text. Add the translation to texts.

```rr
actor Customer
  id uuid

app(Customer) Shop "Shop"
  language de
  Menu
    Home

screen Home
  Toolbar

texts en
  Customer "Customer"
```

```rr
actor Customer
  id uuid

app(Customer) Shop "Shop"
  language de
  Menu
    Home

screen Home
  Toolbar

texts en
  Shop "Shop"
```
