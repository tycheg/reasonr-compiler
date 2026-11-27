## 26. Example 2: a bank transfer

Collecting all validation errors, `after`, checking that another entity exists, an atomic change of two accounts. The external system is antifraud: it scores every transfer after commit and freezes a suspicious account with a public command. The client app has accounts, a transfer and a profile; the target account number is entered with an own primitive that UI developers build.

```rr
# bank/contexts/Accounts/bank.rr

role compliance
role antifraud

value AccountRow
  accountId Account.Id
  balance decimal
  frozen bool

value ClientProfile
  fullName string?

actor Client
  id uuid
  fullName string? personal

  command(self) Join(clientName string personal)
    if [id]
      alreadyJoined()
      return
    joined(clientName: clientName)

  ## Change the name
  command(self) Rename(clientName string personal)
    if not [id]
      notJoined()
      return
    renamed(clientName: clientName)

  ## Delete the client's personal data: the accounts and the transfer history stay
  command(self, compliance) Forget()
    if not [id]
      notJoined()
      return
    forget id
    forgotten()

  event joined(clientName string? personal)
    fullName = clientName

  event renamed(clientName string? personal)
    fullName = clientName

  event forgotten()

  error alreadyJoined
  error notJoined

  query(Client) Profile() ClientProfile
    let c = [me]
    if c is none
      notJoined()
      return
    return ClientProfile(fullName: c.fullName)

aggregate Account
  id uuid
  owner Client.Id?
  balance decimal
  overdraft decimal
  frozen bool

  index byOwner(owner)

  ## Open an account: the bank issues the number, the client learns it from the response
  command(Client) create Open()
    accountCreated(ownerId: me)
    accountOpened(accountId: id)

  ## Transfer to another account
  command(owner) Transfer(target Account.Id, sum decimal, comment string)
    if sum <= 0
      invalidSum(sum: sum)
    if target == id
      sameAccount()
    if not [target]
      accountNotFound(accountId: target)
    if frozen
      accountFrozen(accountId: id)
    if comment.length > 140
      commentTooLong(max: 140)
    if hasErrors
      return                                   # the client gets all errors at once

    withdrawn(amount: sum, target: target, note: comment)
    if after.balance < -overdraft              # the state after withdrawn
      insufficientFunds(available: balance + overdraft)
      return

    [target].deposit(amount: sum, source: id)                  # an error of the target cancels the whole transfer
    Antifraud.score(accountId: id, target: target, sum: sum)   # sent after commit
    transferDone(newBalance: after.balance)

  command deposit(amount decimal, source Account.Id)
    if frozen
      accountFrozen(accountId: id)
      return
    deposited(amount: amount, source: source)

  command(compliance, antifraud) Freeze(reason string)
    frozenBy(reason: reason)

  ## The allowed overdraft of the account
  command(compliance) SetOverdraft(limit decimal)
    if limit < 0
      invalidSum(sum: limit)
      return
    overdraftSet(limit: limit)

  event accountCreated(ownerId Client.Id)
    owner = ownerId

  event withdrawn(amount decimal, target Account.Id, note string)
    balance = balance - amount

  event deposited(amount decimal, source Account.Id)
    balance = balance + amount

  event frozenBy(reason string)
    frozen = true

  event overdraftSet(limit decimal)
    overdraft = limit

  notification accountOpened(accountId Account.Id)
  notification transferDone(newBalance decimal)

  error invalidSum(sum decimal)
  error sameAccount
  error accountFrozen(accountId Account.Id)
  error accountNotFound(accountId Account.Id)
  error commentTooLong(max int)
  error insufficientFunds(available decimal)

  query(Client) MyAccounts(pageReq PageRequest) page<AccountRow>
    return [Account]
      where owner == me
      page pageReq
      select AccountRow(accountId: id, balance: balance, frozen: frozen)

  query(Client) Details(accountId Account.Id) AccountRow
    let a = [accountId]
    if a is none or a.owner != me
      accountNotFound(accountId: accountId)
      return
    return AccountRow(accountId: accountId, balance: a.balance, frozen: a.frozen)

## Antifraud: scores transfers, freezes a suspicious account (Freeze)
external(antifraud) Antifraud
  request score(accountId Account.Id, target Account.Id, sum decimal)
```

The antifraud fake for tests and local runs ([§18.1](03-systems-access-ui.md#181-fakes-of-external-systems)):

```rr
# bank/contexts/Accounts/antifraud.fake.rr

fake Antifraud
  limit decimal

  ## Transfers larger than limitAmount are suspicious
  command Setup(limitAmount decimal)
    configured(limitAmount: limitAmount)

  event configured(limitAmount decimal)
    limit = limitAmount

  request score(accountId Account.Id, target Account.Id, sum decimal)
    if sum > limit
      [accountId].Freeze(reason: "suspicious transfer")
```

The client app ([§22](03-systems-access-ui.md#22-interface-ui)). A client does not search other people's accounts — they know the target account number. The catalog has no primitive for such input, so the model declares its own `AccountNumber` (in `primitives/`, §23); until UI developers implement it, the automatic UI is drawn in its place:

```rr
# bank/primitives/primitives.rr

## Account number: input of an identifier the client knows but cannot search for
primitive AccountNumber<K id>
  binds input K
```

```rr
# bank/apps/Bank/app.rr

app(Client) Bank "Bank"
  language en
  default Account.Id AccountNumber
  Menu
    MyAccounts
    Profile
```

```rr
# bank/contexts/Accounts/ui.rr
# Bank client screens (apps/Bank): accounts, a transfer, the profile (§22)

screen MyAccounts "My accounts"
  Button Account.Open "Open an account"         # the engine issues the number
  List Account.MyAccounts
    balance
    frozen
    on open AccountCard

screen AccountCard(accountId Account.Id) "Account"
  Card Account.Details
    Actions
      Edit Account.Transfer "Transfer"

screen Profile "Profile"
  Edit Client.Join "Become a client"
  Card Client.Profile
    Actions
      Edit Client.Rename "Change name"
      Confirm Client.Forget
```

Bank acceptance tests ([§27](08-tests.md#27-acceptance-tests)):

```rr
# bank/contexts/Accounts/bank.test.rr

test "All validation errors come at once"
  let c1 = new Client.Id
  let a1 = new Account.Id
  let a2 = new Account.Id
  given
    [c1].Join(clientName: "Anna") as c1
    [a1].Open() as c1
  when
    [a1].Transfer(target: a2, sum: -5, comment: "") as c1
  then
    error invalidSum(sum: -5)
    error accountNotFound(accountId: a2)

test "A transfer within the overdraft"
  let c1 = new Client.Id
  let c2 = new Client.Id
  let a1 = new Account.Id
  let a2 = new Account.Id
  given
    [c1].Join(clientName: "Anna") as c1
    [c2].Join(clientName: "Boris") as c2
    [a1].Open() as c1
    [a2].Open() as c2
    [a1].SetOverdraft(limit: 500) as compliance
  when
    [a1].Transfer(target: a2, sum: 200, comment: "for lunch") as c1
  then
    notification transferDone(newBalance: -200) to c1
    Account.Details(accountId: a1) as c1 matches (balance: -200)
    Account.Details(accountId: a2) as c2 matches (balance: 200)

test "A transfer beyond the overdraft is rejected as a whole"
  let c1 = new Client.Id
  let c2 = new Client.Id
  let a1 = new Account.Id
  let a2 = new Account.Id
  given
    [c1].Join(clientName: "Anna") as c1
    [c2].Join(clientName: "Boris") as c2
    [a1].Open() as c1
    [a2].Open() as c2
    [a1].SetOverdraft(limit: 100) as compliance
  when
    [a1].Transfer(target: a2, sum: 150, comment: "") as c1
  then
    error insufficientFunds(available: 100)
    Account.Details(accountId: a1) as c1 matches (balance: 0)

test "An error of the target cancels the transfer"
  let c1 = new Client.Id
  let c2 = new Client.Id
  let a1 = new Account.Id
  let a2 = new Account.Id
  given
    [c1].Join(clientName: "Anna") as c1
    [c2].Join(clientName: "Boris") as c2
    [a1].Open() as c1
    [a2].Open() as c2
    [a1].SetOverdraft(limit: 500) as compliance
    [a2].Freeze(reason: "review") as compliance
  when
    [a1].Transfer(target: a2, sum: 100, comment: "") as c1
  then
    error accountFrozen(accountId: a2)
    Account.Details(accountId: a1) as c1 matches (balance: 0)

test "Another client's account is not accessible"
  let c1 = new Client.Id
  let c2 = new Client.Id
  let a1 = new Account.Id
  let a2 = new Account.Id
  given
    [c1].Join(clientName: "Anna") as c1
    [c2].Join(clientName: "Boris") as c2
    [a1].Open() as c1
    [a2].Open() as c2
  when
    [a1].Transfer(target: a2, sum: 1, comment: "") as c2
  then
    error forbidden

test "Another client's account is not visible"
  let c1 = new Client.Id
  let c2 = new Client.Id
  let a1 = new Account.Id
  given
    [c1].Join(clientName: "Anna") as c1
    [c2].Join(clientName: "Boris") as c2
    [a1].Open() as c1
  when
    Account.Details(accountId: a1) as c2
  then
    error accountNotFound(accountId: a1)

test "After forgetting, the client name is unreadable and the accounts stay"
  let c1 = new Client.Id
  let a1 = new Account.Id
  given
    [c1].Join(clientName: "Anna") as c1
    [a1].Open() as c1
  when
    [c1].Forget() as compliance
  then
    Client.Profile() as c1 matches (fullName: none)
    Account.Details(accountId: a1) as c1 matches (balance: 0)

test "After forgetting, the client gives their name again"
  let c1 = new Client.Id
  given
    [c1].Join(clientName: "Anna") as c1
    [c1].Forget() as c1
  when
    [c1].Rename(clientName: "Anna Smith") as c1
  then
    Client.Profile() as c1 matches (fullName: "Anna Smith")
```

Antifraud scenarios run on the fake: the test does not call the system's answer itself.

```rr
# bank/contexts/Accounts/antifraud.test.rr

test "Antifraud freezes the account after a large transfer"
  let c1 = new Client.Id
  let c2 = new Client.Id
  let a1 = new Account.Id
  let a2 = new Account.Id
  given
    Antifraud.Setup(limitAmount: 1000)
    [c1].Join(clientName: "Anna") as c1
    [c2].Join(clientName: "Boris") as c2
    [a1].Open() as c1
    [a2].Open() as c2
    [a1].SetOverdraft(limit: 5000) as compliance
  when
    [a1].Transfer(target: a2, sum: 2000, comment: "for a car") as c1
  then
    request Antifraud.score(accountId: a1, target: a2, sum: 2000)
    Account.Details(accountId: a1) as c1 matches (balance: -2000, frozen: true)

test "A transfer within the antifraud limit does not freeze the account"
  let c1 = new Client.Id
  let c2 = new Client.Id
  let a1 = new Account.Id
  let a2 = new Account.Id
  given
    Antifraud.Setup(limitAmount: 1000)
    [c1].Join(clientName: "Anna") as c1
    [c2].Join(clientName: "Boris") as c2
    [a1].Open() as c1
    [a2].Open() as c2
    [a1].SetOverdraft(limit: 5000) as compliance
  when
    [a1].Transfer(target: a2, sum: 300, comment: "") as c1
  then
    Account.Details(accountId: a1) as c1 matches (balance: -300, frozen: false)
```
