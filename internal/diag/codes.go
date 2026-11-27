package diag

// The code registry. Ranges: RFC §28
// Every code must be covered by at least one `# want "CODE"` test.

// E00xx — lexis and syntax.
var (
	E0001 = errorSpec("E0001",
		"unknown character",
		"unknown character %q")
	E0002 = errorSpec("E0002",
		"tab in indentation",
		"tab in indentation: use spaces")
	E0003 = errorSpec("E0003",
		"indentation step",
		"a block must be indented exactly 2 spaces more than its parent, got %d")
	E0004 = errorSpec("E0004",
		"inconsistent indentation",
		"indentation does not match any enclosing block")
	E0005 = errorSpec("E0005",
		"unterminated string",
		"string is not terminated before the end of the line")
	E0006 = errorSpec("E0006",
		"invalid date or time literal",
		"invalid %s literal: expected format %s")
	E0007 = errorSpec("E0007",
		"invalid number",
		"invalid number %q: %s")
	E0008 = errorSpec("E0008",
		"invalid string interpolation",
		"invalid string interpolation: %s")
	E0009 = errorSpec("E0009",
		"invalid escape sequence",
		"invalid escape sequence %q: allowed are \\\" \\\\ \\n \\t \\u{…}")
	E0010 = errorSpec("E0010",
		"positional argument",
		"arguments of %s are passed by name: name: value")

	E0011 = errorSpec("E0011",
		"expression is not a statement",
		"expression is not a statement: expected a call, an assignment or a keyword")
	E0012 = errorSpec("E0012",
		"syntax error",
		"expected %s, found %s")
	E0013 = errorSpec("E0013",
		"too many errors",
		"too many syntax errors, the rest are not shown")
	E0014 = errorSpec("E0014",
		"invalid assignment target",
		"only a variable or a field can be assigned by name")
	E0015 = errorSpec("E0015",
		"unexpected indentation",
		"unexpected indentation: the previous line does not open a block")
	E0016 = errorSpec("E0016",
		"missing body",
		"%s must be followed by a body indented by 2 spaces")
	E0017 = errorSpec("E0017",
		"chained comparison",
		"comparisons cannot be chained: use and")
	E0018 = errorSpec("E0018",
		"duplicate clause",
		"clause %s is already specified")
	E0020 = errorSpec("E0020",
		"test section order",
		"section %s is out of order: the order is feature, let, given, when, then")
	E0021 = errorSpec("E0021",
		"search clause order",
		"clause %s is out of order: the order is where, sort, page or limit, select")
)

// E01xx — names.
var (
	E0101 = errorSpec("E0101",
		"duplicate declaration",
		"%q is already declared in the module: %s")
	E0102 = errorSpec("E0102",
		"duplicate member",
		"%q already has %s %q")
	E0103 = errorSpec("E0103",
		"name coincides with a field",
		"%q coincides with a field name: fields, parameters and variables must have different names")
	E0104 = errorSpec("E0104",
		"name already declared in the method",
		"%q is already declared above: parameters and variables of a method must not repeat or shadow each other")
	E0106 = errorSpec("E0106",
		"naming style",
		"%s %q must start with %s letter")
	E0107 = errorSpec("E0107",
		"unknown type",
		"unknown type %q")
	E0108 = errorSpec("E0108",
		"built-in name",
		"%q is a built-in value and cannot name a field, parameter or variable")
	E0109 = errorSpec("E0109",
		"type arguments",
		"%s")
	E0110 = errorSpec("E0110",
		"missing id",
		"%q has no id line: declare it as id uuid, id sequence, id string or id <value type>")
	E0111 = errorSpec("E0111",
		"invalid id",
		"%s")
)

// E01xx (continued), E02xx — types.
var (
	E0105 = errorSpec("E0105",
		"ambiguous enum value",
		"%q is both a %s name and a value of enum %s: write %s.%s")
	E0201 = errorSpec("E0201",
		"unknown name",
		"unknown name %q")
	E0202 = errorSpec("E0202",
		"type mismatch",
		"expected %s, got %s")
	E0203 = errorSpec("E0203",
		"missing arguments",
		"missing arguments of %s: %s")
	E0204 = errorSpec("E0204",
		"unknown argument",
		"%s has no parameter %q")
	E0205 = errorSpec("E0205",
		"repeated argument",
		"argument %q is already passed")
	E0206 = errorSpec("E0206",
		"command call on an optional id",
		"%s may be none: write [x]?.%s(…) so the call only runs if the entity exists")
	E0207 = errorSpec("E0207",
		"needless ?.",
		"%s cannot be none: use '.' instead of '?.'")
	E0208 = errorSpec("E0208",
		"invalid delete argument",
		"delete removes a timer (Timer.Id) or a projection row ([martId]), got %s")
	E0209 = errorSpec("E0209",
		"notification recipient is not an actor",
		"a notification recipient must be an actor id, got %s")
	E0210 = errorSpec("E0210",
		"id field without default",
		"an id has no default value: field %q must be %s?")
	E0211 = errorSpec("E0211",
		"value field without default",
		"%s has no default value: field %q must be %s? or have '= value'")
	E0212 = errorSpec("E0212",
		"non-constant default",
		"a default value must be a constant: literals, enum values, value constructors")
	E0213 = errorSpec("E0213",
		"unused value",
		"the value of %s is not used")
	E0214 = errorSpec("E0214",
		"invalid statement",
		"%s")
	E0215 = errorSpec("E0215",
		"id cannot be generated",
		"new %s.Id: %q ids are provided by the caller (id %s) and cannot be generated")
	E0216 = errorSpec("E0216",
		"invalid id conversion",
		"%s.Id(…) only converts %s")
	E0217 = errorSpec("E0217",
		"fields of another entity",
		"fields of %s are not available here: another entity can only be checked for existence and sent commands")
	E0218 = errorSpec("E0218",
		"cannot infer type",
		"cannot infer the type of %s here: declare the variable type")
	E0219 = errorSpec("E0219",
		"value may be none",
		"%s may be none: check is not none first or use ?.")
	E0220 = errorSpec("E0220",
		"invalid operator",
		"operator %s cannot be applied to %s")
	E0221 = errorSpec("E0221",
		"unknown member",
		"%s has no %s")
	E0222 = errorSpec("E0222",
		"tests and demos only",
		"%s is for tests and demo scenarios only: a model takes no random values and iterates no ranges")
)

// E03xx — effects and bounded contexts.
var (
	E0301 = errorSpec("E0301",
		"field assigned outside an event",
		"field %q can only be assigned in an event: call an event that assigns it")
	E0302 = errorSpec("E0302",
		"not allowed in an event",
		"an event may only assign fields: events must be replayable")
	E0303 = errorSpec("E0303",
		"call not allowed in this context",
		"%s cannot %s")
	E0304 = errorSpec("E0304",
		"built-in value not available",
		"%s cannot use %s: %s")
	E0305 = errorSpec("E0305",
		"incomplete projection row",
		"projection row %s is written whole: missing fields %s")
)

// E04xx — call graph.
var (
	E0401 = errorSpec("E0401",
		"command recursion",
		"commands call each other in a cycle: %s")
	E0402 = errorSpec("E0402",
		"timer run is not a command of this entity",
		"run: expected a call of a command of this entity")
)

// E05xx — queries, indexes, uniqueness, execution paths.
var (
	E0501 = errorSpec("E0501",
		"search condition cannot use an index",
		"where is a conjunction of 'field op value' conditions joined by and: %s is not allowed")
	E0502 = errorSpec("E0502",
		"condition value depends on the record",
		"the right side of a condition must be parameters, me, now or literals, but %q is a record field")
	E0503 = errorSpec("E0503",
		"!= is not supported by indexes",
		"indexes cannot search by 'not equal': list the wanted values with in")
	E0504 = errorSpec("E0504",
		"no suitable index",
		"no index for searching %s: %s")
	E0505 = errorSpec("E0505",
		"search without page or limit",
		"a search returns a page (page) or a bounded list (limit ≤ 100)")
	E0506 = errorSpec("E0506",
		"limit over 100",
		"limit must be at most 100, got %s: use page for larger results")
	E0509 = errorSpec("E0509",
		"invalid query result type",
		"a query returns a value type, page<value> or list<value>, not %s")
	E0510 = errorSpec("E0510",
		"record of another type",
		"records of %s are only readable in queries of %s: call its query")
	E0511 = errorSpec("E0511",
		"unique error not declared",
		"error %q is not declared in %q")
	E0512 = errorSpec("E0512",
		"unique error parameters",
		"parameter %q of a unique error must be named after the last segment of an index path and have its type")
	E0507 = errorSpec("E0507",
		"query does not return a value",
		"a query must return a value or raise an error before return")
	E0508 = errorSpec("E0508",
		"missing result",
		"%s must return a value on every path: return value")
)

// E07xx — UI (RFC §22).
var (
	E0701 = errorSpec("E0701",
		"invalid primitive declaration",
		"%s")
	E0702 = errorSpec("E0702",
		"binding does not fit the primitive",
		"%s")
	E0703 = errorSpec("E0703",
		"binding is not a public method or screen",
		"%s")
	E0704 = errorSpec("E0704",
		"name not found in the layout",
		"%s")
	E0705 = errorSpec("E0705",
		"command target is ambiguous",
		"%s")
	E0706 = errorSpec("E0706",
		"value type does not match the parameter",
		"%s")
	E0707 = errorSpec("E0707",
		"no default primitive",
		"%s")
	E0708 = errorSpec("E0708",
		"argument has no source",
		"%s")
	E0709 = errorSpec("E0709",
		"invalid on transition",
		"%s")
	E0710 = errorSpec("E0710",
		"screen parameters are not filled",
		"%s")
	E0711 = errorSpec("E0711",
		"me on a screen of different apps",
		"%s")
	E0712 = errorSpec("E0712",
		"invalid default",
		"%s")
	E0713 = errorSpec("E0713",
		"invalid text key",
		"%s")
	E0714 = errorSpec("E0714",
		"invalid text placeholder",
		"%s")
	E0715 = errorSpec("E0715",
		"invalid app",
		"%s")
	E0716 = errorSpec("E0716",
		"invalid primitive parameter",
		"%s")
)

// E08xx — tests.
var (
	E0801 = errorSpec("E0801",
		"invalid given step",
		"given only allows public command calls, time offset and let, for, while, if statements")
	E0802 = errorSpec("E0802",
		"when must have one action",
		"%s")
	E0803 = errorSpec("E0803",
		"as is not an actor or a role",
		"as %s: expected a variable holding an actor id or a role")
	E0804 = errorSpec("E0804",
		"invalid then check",
		"%s")
	E0805 = errorSpec("E0805",
		"a demo scenario has only given",
		"%s in a demo scenario (*.demo.rr): a scenario fills a tenant and checks nothing — write checks in *.test.rr tests")
)

// W1xxx — warnings.
var (
	W1001 = warningSpec("W1001",
		"unused declaration",
		"%s %q is never used")
	W1002 = warningSpec("W1002",
		"unreachable code",
		"code after %s is never executed")
	W1020 = warningSpec("W1020",
		"forget without personal data",
		"%s has no personal data: forget removes nothing")
	W1021 = warningSpec("W1021",
		"non-personal parameter with personal data",
		"parameter %q flows into personal %q: mark it personal so the engine does not log it")
	W1003 = warningSpec("W1003",
		"while without an exit",
		"the while loop changes no condition variables and has no break or return: it may never end")
	W1004 = warningSpec("W1004",
		"commands in a loop over a collection",
		"a command call in a loop over %q: the collection is unbounded and the unit of work may become huge")
	W1005 = warningSpec("W1005",
		"float for money",
		"%q looks like money, and float loses cents: use decimal")
	W1006 = warningSpec("W1006",
		"notification without recipient from a timer",
		"command %q is only run by a timer and has no human initiator: set the notification recipient with to")
	W1010 = warningSpec("W1010",
		"public command without access",
		"command %q is available to any authenticated user: is that intended? Declare access in parentheses")
	W1030 = warningSpec("W1030",
		"external system cannot answer",
		"external system %s answers with role %s, but no public command is available to that role")
	W1301 = warningSpec("W1301",
		"node is not available to anyone in the app",
		"%s is not available to anyone who enters app %s: nobody will see the node")
	W1302 = warningSpec("W1302",
		"unreachable screen",
		"screen %s cannot be reached from any app: add it to a menu, a Link or an on transition")
	W1303 = warningSpec("W1303",
		"missing text translation",
		"no translation of %q into %s: add it to texts %s")
)

// E06xx — access.
var (
	E0601 = errorSpec("E0601",
		"self outside an actor",
		"self is only allowed in the access list of an actor command")
	E0602 = errorSpec("E0602",
		"access field is not an actor id",
		"field %q has type %s, but access needs a field holding an actor id")
	E0603 = errorSpec("E0603",
		"role coincides with a field",
		"%q is both a role and a field, so the access list is ambiguous; rename the field")
	E0604 = errorSpec("E0604",
		"unknown name in access list",
		"unknown name %q in access list: expected self, a field, an actor or a role")
	E0605 = errorSpec("E0605",
		"access on a private method",
		"only public methods (capitalized) have access lists, but %q is private")
	E0606 = errorSpec("E0606",
		"field in query or view access",
		"%q is a field: field-based access is only allowed for commands")
)

// E09xx — personal data.
var (
	E0901 = errorSpec("E0901",
		"personal data leak",
		"personal data (%s) flows into non-personal %q: mark it personal")
	E0902 = errorSpec("E0902",
		"different personal data subject",
		"%q holds data of subject %s, but data of %s flows here")
	E0903 = errorSpec("E0903",
		"personal data into a non-personal command parameter",
		"parameter %q of command %s receives personal data: mark it personal")
	E0904 = errorSpec("E0904",
		"personal value is not optional",
		"%q is personal data and reads as none after forget: the type must be %s?")
	E0905 = errorSpec("E0905",
		"personal field in an index",
		"an index searches the personal field %q by equality only (== and in), without sorting or desc")
	E0906 = errorSpec("E0906",
		"personal projection field without its subject",
		"%q is a personal field of projection %s with subject %q: pass %q too, otherwise the engine does not know whose key encrypts it")
)

// E10xx — module and extensions.
var (
	E1002 = errorSpec("E1002",
		"nothing to extend",
		"nothing to extend: the base model has no %s %q")
	E1003 = errorSpec("E1003",
		"name conflict in extension",
		"%q is already declared in the base model: an extension can only add new members")
	E1004 = errorSpec("E1004",
		"rule signature changed",
		"rule %q is overridden with a different signature: parameters and result type must match the base rule")
	E1005 = errorSpec("E1005",
		"rule without a base declaration",
		"%q has no rule %q in the base model: an extension can only override declared rules")
	E1006 = errorSpec("E1006",
		"reaction to an unknown event",
		"%q has no event %q")
	E1007 = errorSpec("E1007",
		"excluding an unknown test",
		"the base model has no test %q")
	E1009 = errorSpec("E1009",
		"extension in a base file",
		"'%s' is only allowed in extension files extensions/<name>/")
	E1011 = errorSpec("E1011",
		"screen part is ambiguous",
		"screen %s has %[3]d lines of part %[2]s: an extension needs exactly one")
	E1010 = errorSpec("E1010",
		"projection extension",
		"projection %q cannot be extended: the base model writes its rows whole and would reset the extension's fields; declare a projection of your own")
)

// E11xx — model version compatibility (reasonr check --against).
var (
	E1101 = errorSpec("E1101",
		"manual migration required",
		"%s: %s — existing data cannot be read this way; add a manual migration")
	E1102 = errorSpec("E1102",
		"incompatible event change",
		"event %s: %s — stored events would become unreadable; declare a new event")
	E1103 = errorSpec("E1103",
		"field became required",
		"field %s was %s and is now %s: existing data may contain none")
	E1104 = errorSpec("E1104",
		"API of a version removed",
		"API version %s: no longer present: %s, so clients of this version will get an error; mark a renamed item with its old name (was), remove items via reasonr api deprecate")
	E1105 = errorSpec("E1105",
		"new required API parameter",
		"API version %s: %s: new required parameter %s that clients of this version do not pass; give it a default or declare a new method")
	E1106 = errorSpec("E1106",
		"type in an API version changed",
		"API version %s: %s had type %s and now has %s, which clients of this version cannot read")
	E1107 = errorSpec("E1107",
		"API contract names an unknown method",
		"api/contract.json publishes %s, but the model has no such public command or query")
)

// E12xx — external systems and fakes.
var (
	E1201 = errorSpec("E1201",
		"external system role",
		"the parentheses of external system %s hold the one role it answers with, declared by role; %q does not fit")
	E1202 = errorSpec("E1202",
		"timeout parameter is not a timer",
		"the timeout parameter of request %s is a model timer: type Timer.Id or Timer.Id?, not %s")
	E1203 = errorSpec("E1203",
		"request deadline is a timer",
		"'timeout %s' is no longer written on a request: declare a parameter timeout Timer.Id, schedule a timer and pass it to the request — the timer is both the deadline and the model's reaction")
	E1204 = errorSpec("E1204",
		"fake outside a *.fake.rr file",
		"fake is declared only in *.fake.rr files of the base module: fakes are built into the test program only")
	E1205 = errorSpec("E1205",
		"non-fake declaration in a *.fake.rr file",
		"a *.fake.rr file declares only fakes (fake): move the rest to model or test files")
	E1207 = errorSpec("E1207",
		"request implementation does not match its declaration",
		"an implementation in a fake repeats the request declaration without timeout: request %s")
	E1208 = errorSpec("E1208",
		"command not available to the system role",
		"a fake calls model commands with role %s, like the real system through the API, and %s is not available to it: %s")
	E1209 = errorSpec("E1209",
		"a fake sees the model only through the API",
		"a fake behaves like an external system and sees the model only through its API: %s is not available in a fake")
	E1210 = errorSpec("E1210",
		"fake command with as",
		"fake commands are called by the test only, without as: %s")
	E1206 = errorSpec("E1206",
		"fake without an external system with a role",
		"fake %s replaces an external system in tests and answers with its role: declare external(role) %s")
)

// E14xx — project structure: part directories and declaration places.
var (
	E1401 = errorSpec("E1401",
		"file outside project parts",
		"file %s lies outside the project parts: move it to contexts/<Context>/, apps/<App>/ or primitives/")
	E1402 = errorSpec("E1402",
		"context directory name",
		"context directory %s: a context name is one capitalized word, e.g. Ordering")
	E1403 = errorSpec("E1403",
		"app of an apps directory",
		"the directory apps/%[1]s declares exactly one app, %[1]s")
	E1404 = errorSpec("E1404",
		"declaration outside its project part",
		"%s is declared in %s")
	E1406 = errorSpec("E1406",
		"type named like a context",
		"%q is the name of a context: name the type differently so that a short name points to one thing")
	E1407 = errorSpec("E1407",
		"not accessible from another context",
		"%s belongs to context %s: another context sees its model only in its own ACL (acl %[2]s): entities with their public commands and events, values and enums")
	E1412 = errorSpec("E1412",
		"acl outside an *.acl.rr file",
		"acl %s is declared in an *.acl.rr file of its context, e.g. %s.acl.rr")
	E1413 = errorSpec("E1413",
		"not an acl in an *.acl.rr file",
		"an *.acl.rr file declares only acl")
	E1414 = errorSpec("E1414",
		"ACL source is not another context",
		"acl %s: the source is another context of the project, and the ACL lies in the directory of its own context")
	E1415 = errorSpec("E1415",
		"reaction to another context's event outside an ACL",
		"on %s: an event of another context is handled by an ACL of your context, acl %s in an *.acl.rr file")
	E1416 = errorSpec("E1416",
		"ACL event is not a source event",
		"on %s: an ACL reaction names an event of a source entity by its full path, e.g. on %s.Order.placed")
	E1417 = errorSpec("E1417",
		"invalid actor translation",
		"actor %s as %s: the left side is an actor of context %s, the right side is an actor of your context with the same id kind")
	E1418 = errorSpec("E1418",
		"enum cannot be translated",
		"%s(…) translates a value of %s by name, but %[1]s has no value %[3]s")
	E1419 = errorSpec("E1419",
		"top-level entity",
		"%s: an aggregate is declared at the top level, aggregate %[1]s; entity is an inner entity of an aggregate: entity Aggregate.%[1]s")
	E1420 = errorSpec("E1420",
		"no aggregate for the inner entity",
		"entity %s.%s: %[1]s is not an aggregate of this context")
	E1421 = errorSpec("E1421",
		"inner entity member",
		"%s: inner entity %s has only private commands, no queries or indexes; public commands and queries belong to the aggregate")
	E1422 = errorSpec("E1422",
		"inner entity outside its aggregate",
		"%s is addressed only inside aggregate %s: from outside, call the aggregate's commands")
	E1423 = errorSpec("E1423",
		"inner entity id outside its aggregate",
		"the id of %s is stored and passed only by aggregate %s and its inner entities: elsewhere it addresses nothing")
	E1424 = errorSpec("E1424",
		"invalid create command",
		"create %s: %s")
)

// E15xx — feature flags (RFC §23.4).
var (
	E1501 = errorSpec("E1501",
		"invalid feature flag date",
		"the creation date of flag %s is a YYYY-MM-DD string with a valid date, e.g. \"2026-11-07\", got %s")
	E1502 = errorSpec("E1502",
		"nested feature flag",
		"flag %s is read inside a branch of flag %s: flags do not nest")
	E1503 = errorSpec("E1503",
		"two feature flags in one condition",
		"the condition already reads flag %s: a condition reads one flag")
	E1504 = errorSpec("E1504",
		"feature flag not available",
		"%s cannot read flag %s: %s")
	E1505 = errorSpec("E1505",
		"unknown feature flag",
		"no flag %q: a flag is declared in a context with feature %s \"YYYY-MM-DD\"")
	E1506 = errorSpec("E1506",
		"too many feature flags",
		"the model has %d flags, at most %d are allowed: remove rolled-out flags from the code (reasonr feature remove)")
)

// W15xx — feature flag warnings.
var (
	W1501 = warningSpec("W1501",
		"feature flag without a test",
		"no test enables flag %s: cover the code under the flag with a test that starts with feature %s")
)

// W11xx — version compatibility warnings.
var (
	W1101 = warningSpec("W1101",
		"command removed",
		"command %s was removed: if scheduled timers refer to it, a migration is needed")
	W1102 = warningSpec("W1102",
		"data will be lost",
		"%s was removed: its data will be lost")
	W1103 = warningSpec("W1103",
		"new unique index",
		"unique index %s: the engine will check existing data for duplicates during migration and stop if it finds any")
)
