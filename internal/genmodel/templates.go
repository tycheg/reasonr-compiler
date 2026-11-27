package genmodel

// Templates of model parts: an average aggregate is about 200 lines with tests.

var typesTmpl = tmpl(`
# contexts/{{.Name}}/types.rr
{{if eq .N 0}}
## An auditor sees the items of all bounded contexts
role auditor
{{end}}
enum Stage
  draft
  approved
  rejected
  archived

## A member of bounded context {{.Name}}
actor Member
  id uuid
  nick string
  joinedAt timestamp

  command(self) Join(nickname string)
    if [id]
      alreadyJoined()
      return
    if nickname.length == 0
      emptyNick()
      return
    joined(memberNick: nickname, at: now)

  event joined(memberNick string, at timestamp)
    nick = memberNick
    joinedAt = at

  query(auditor) Profile(memberId Member.Id) MemberRow
    let m = [memberId]
    if m is none
      notMember()
      return
    return MemberRow(nick: m.nick, joinedAt: m.joinedAt)

  error alreadyJoined
  error emptyNick
  error notMember

value MemberRow
  nick string
  joinedAt timestamp
`)

var aggTmpl = tmpl(`
# contexts/{{.Ctx}}/{{lower .Name}}.rr

value {{.Name}}Row
  itemId {{.Name}}.Id
  title string
  stage Stage
  amount decimal
  qty int

## Item {{.Name}} of bounded context {{.Ctx}}
aggregate {{.Name}}
  id uuid
  owner Member.Id?
  title string
  stage Stage
  amount decimal
  qty int
{{- if not .Last}}
  touches int
{{- end}}
  note string?
  updatedAt timestamp?
{{- if .Prev}}
  ref {{.Prev}}.Id?
{{- end}}

  index byOwner(owner, updatedAt desc)
  index byStage(stage, updatedAt desc)

  ## Create an item
  command(Member) Create(name string, price decimal, count int)
    if [id]
      {{.P}}Exists(itemId: id)
      return
    if name.length == 0
      {{.P}}EmptyTitle()
    if price < 0
      {{.P}}NegativeAmount(amount: price)
    if count <= 0
      {{.P}}BadQty(qty: count)
    if hasErrors
      return
    created(ownerId: me, itemTitle: name, itemAmount: price, itemQty: count, at: now)
    {{.P}}Created(itemId: id)

  ## Rename
  command(owner) Rename(name string)
    if name.length == 0
      {{.P}}EmptyTitle()
      return
    if stage == archived
      {{.P}}Archived(itemId: id)
      return
    renamed(itemTitle: name, at: now)

  ## Change the amount
  command(owner) SetAmount(price decimal)
    if price < 0
      {{.P}}NegativeAmount(amount: price)
      return
    if stage != draft
      {{.P}}NotDraft(stage: stage)
      return
    amountSet(itemAmount: price, at: now)

  ## Add quantity
  command(owner) AddQty(delta int)
    if delta == 0 or qty + delta <= 0
      {{.P}}BadQty(qty: qty + delta)
      return
    qtyChanged(itemQty: qty + delta, at: now)

  ## Approve
  command(owner, auditor) Approve(comment string)
    if stage != draft
      {{.P}}NotDraft(stage: stage)
      return
    if amount > 1000000
      {{.P}}TooExpensive(total: amount)
      return
    staged(newStage: approved, comment: comment, at: now)
    {{.P}}Approved(itemId: id, total: amount)

  ## Reject
  command(owner, auditor) Reject(comment string)
    if stage != draft
      {{.P}}NotDraft(stage: stage)
      return
    staged(newStage: rejected, comment: comment, at: now)

  ## Archive
  command(owner) Archive()
    if stage == archived
      {{.P}}Archived(itemId: id)
      return
    staged(newStage: archived, comment: "", at: now)
{{- if .Prev}}

  ## Link to item {{.Prev}}: it records the touch
  command(owner) Link(target {{.Prev}}.Id)
    if not [target]
      {{.P}}NotFound()
      return
    linked(target: target, at: now)
    [target].touch(by: title)
{{- end}}

{{- if not .Last}}

  command touch(by string)
    touched(by: by, at: now)
{{- end}}
{{- if .Mirror}}

  ## A copy of an item from a neighboring bounded context (ACL)
  command mirror(name string, price decimal)
    if [id]
      renamed(itemTitle: name, at: now)
      return
    created(ownerId: none, itemTitle: name, itemAmount: price, itemQty: 1, at: now)
{{- end}}

  event created(ownerId Member.Id?, itemTitle string, itemAmount decimal, itemQty int, at timestamp)
    owner = ownerId
    title = itemTitle
    stage = draft
    amount = itemAmount
    qty = itemQty
    updatedAt = at

  event renamed(itemTitle string, at timestamp)
    title = itemTitle
    updatedAt = at

  event amountSet(itemAmount decimal, at timestamp)
    amount = itemAmount
    updatedAt = at

  event qtyChanged(itemQty int, at timestamp)
    qty = itemQty
    updatedAt = at

  event staged(newStage Stage, comment string, at timestamp)
    stage = newStage
    note = comment
    updatedAt = at
{{- if .Prev}}

  event linked(target {{.Prev}}.Id, at timestamp)
    ref = target
    updatedAt = at
{{- end}}

{{- if not .Last}}

  event touched(by string, at timestamp)
    touches = touches + 1
    note = by
    updatedAt = at
{{- end}}

  notification {{.P}}Created(itemId {{.Name}}.Id)
  notification {{.P}}Approved(itemId {{.Name}}.Id, total decimal)

  error {{.P}}Exists(itemId {{.Name}}.Id)
  error {{.P}}EmptyTitle
  error {{.P}}NegativeAmount(amount decimal)
  error {{.P}}BadQty(qty int)
  error {{.P}}Archived(itemId {{.Name}}.Id)
  error {{.P}}NotDraft(stage Stage)
  error {{.P}}TooExpensive(total decimal)
  error {{.P}}NotFound

  query(Member, auditor) Details(itemId {{.Name}}.Id) {{.Name}}Row
    let x = [itemId]
    if x is none
      {{.P}}NotFound()
      return
    return {{.Name}}Row(itemId: itemId, title: x.title, stage: x.stage, amount: x.amount, qty: x.qty)

  query(Member) Mine(pageReq PageRequest) page<{{.Name}}Row>
    return [{{.Name}}]
      where owner == me
      sort updatedAt desc
      page pageReq
      select {{.Name}}Row(itemId: id, title: title, stage: stage, amount: amount, qty: qty)

  query(auditor) ByStage(wanted Stage, pageReq PageRequest) page<{{.Name}}Row>
    return [{{.Name}}]
      where stage == wanted
      sort updatedAt desc
      page pageReq
      select {{.Name}}Row(itemId: id, title: title, stage: stage, amount: amount, qty: qty)
`)

var testTmpl = tmpl(`
test "{{.Ctx}}.{{.Name}}: create and read"
  let m1 = new Member.Id
  let x1 = new {{.Name}}.Id
  given
    [m1].Join(nickname: "m1") as m1
  when
    [x1].Create(name: "first", price: 10, count: 2) as m1
  then
    notification {{.P}}Created(itemId: x1) to m1
    {{.Name}}.Details(itemId: x1) as m1 matches (title: "first", qty: 2)

test "{{.Ctx}}.{{.Name}}: all creation errors at once"
  let m1 = new Member.Id
  let x1 = new {{.Name}}.Id
  given
    [m1].Join(nickname: "m1") as m1
  when
    [x1].Create(name: "", price: -1, count: 0) as m1
  then
    error {{.P}}EmptyTitle
    error {{.P}}NegativeAmount(amount: -1)
    error {{.P}}BadQty(qty: 0)

test "{{.Ctx}}.{{.Name}}: approval"
  let m1 = new Member.Id
  let x1 = new {{.Name}}.Id
  given
    [m1].Join(nickname: "m1") as m1
    [x1].Create(name: "to approve", price: 100, count: 3) as m1
    [x1].AddQty(delta: 2) as m1
  when
    [x1].Approve(comment: "ok") as m1
  then
    notification {{.P}}Approved(itemId: x1, total: 100) to m1
    {{.Name}}.Details(itemId: x1) as auditor matches (stage: approved, qty: 5)

test "{{.Ctx}}.{{.Name}}: an archived item cannot be renamed"
  let m1 = new Member.Id
  let x1 = new {{.Name}}.Id
  given
    [m1].Join(nickname: "m1") as m1
    [x1].Create(name: "old", price: 1, count: 1) as m1
    [x1].Archive() as m1
  when
    [x1].Rename(name: "new") as m1
  then
    error {{.P}}Archived(itemId: x1)
`)

var aclTmpl = tmpl(`
# contexts/{{.Name}}/{{lower .Prev}}.acl.rr
# Items of {{.Prev}} are copied into {{.Name}} with the same identifier.

acl {{.Prev}}
  on {{.Prev}}.Item0.created
    [Item0.Id(id)].mirror(name: itemTitle, price: itemAmount)

  on {{.Prev}}.Item0.renamed
    [Item0.Id(id)].mirror(name: itemTitle, price: 0)
`)

var aclTestTmpl = tmpl(`
# contexts/{{.Name}}/acl.test.rr

test "{{.Name}}: item {{.Prev}} is copied through the ACL"
  let m1 = new {{.Prev}}.Member.Id
  let x1 = new {{.Prev}}.Item0.Id
  given
    [m1].Join(nickname: "m1") as m1
  when
    [x1].Create(name: "from a neighbor", price: 7, count: 1) as m1
  then
    {{.Name}}.Item0.Details(itemId: {{.Name}}.Item0.Id(x1)) as auditor matches (title: "from a neighbor", amount: 7)
`)

var uiTmpl = tmpl(`
# contexts/{{.Name}}/ui.rr

screen Positions{{.Name}} "My items"
  List Item0.Mine
    title
    stage
    on open Position{{.Name}}

screen Position{{.Name}}(itemId Item0.Id) "Item"
  Card Item0.Details
    Actions
      Edit Item0.Rename "Rename"
      Edit Item0.SetAmount "Amount"
      Confirm Item0.Archive
`)

var appTmpl = tmpl(`
# apps/App{{.Name}}/app.rr

app({{.Name}}.Member) App{{.Name}} "Items of {{.Name}}"
  language de
  Menu
    Positions{{.Name}}
`)
