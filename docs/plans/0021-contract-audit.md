# Plan 0021: the contract audit

Every exported identifier of the contract packages — the compiler root (`reasonr`), the engine's `port`, `port/porttest`, `registry`, `server` — checked for a use outside its module (2026-11-27). The tool loads every module of the workspace with `go/types` and counts as a use: a reference to the identifier, a selector (a field, a method), a struct literal key, an outside type that implements a contract interface (an adapter implementing `port.RowStore`), and a method of a contract type that implements a contract interface used outside (`Model.Execute` through `Interpreter`). Values read through `encoding/json` and calls through the caller's own interfaces are not seen by the tool; the reasons name them. The pilot (its own workspace) uses `port`, `registry`, `server`, `porttest`.

## Changes made by the audit

- `server` is a facade: `Main` and `Run`; the commands are `internal/server`.
- `registry`: only registration stays public (`Register*` with the factory types, `Deps` with `Logger`, `Metrics`, `State`, `Command`, `OpenTool` and `Tool`, `Secret`, `PersonalData`). Finding factories, kinds, codecs, commands, drivers, the UI bundle, the tool opener and the full `Deps` of the built-in adapters are `internal/registry`. `RegisterAuth` and `RegisterListener` became internal: no module adds an authenticator or an ingress.
- `port` → `internal/ports`: the ingress and authentication (`Listener`, `EdgeDeps`, `Authenticator`, `Credentials`, `ErrForbidden`, `ErrUnauthenticated`), tracing (`Tracer`, `Span`), `Checker`, `Nop`, `SystemClock`, `FormatOffset`, `ParseOffset`, the hubs of forgotten subjects and rollout changes (`ForgetHub`, `FeatureHub`, `Listening`), `FeatureExecutor`, `UICatalog`, `RequestSigner`, the value-level stores only the engine implements (`EventStore`, `SchemaStore`, `KeyCache`, `UniqueViolation`).
- `port.Config` is no longer the alias of the whole engine.yaml: it is the part a kind reads (`Engine`, `Settings`, `Stores`, `State`, `Bus`, `Contexts`, `Tenants`, `Limits`, `Projection`).
- `port.Host.Kernel` is gone with the host's kernel: a kind builds its kernel (`NewKernel(KernelConfig())`); `port.Maintainer` is what `reasonrd migrate` and `pii replay` need of the kind's executor.
- `porttest`: `Kind` is unexported (`UseKind` stays); `RowStore` is an interface of what adapter tests use, not the alias of the engine's row bridge with all its methods.
- The compiler root lost the names nothing outside uses and nothing used reaches: `APIField`, `APIMethod`, `APIType`, `CodecError`, `Date`, `Decode` (now `decode`, behind `Open`), `Describe`, `TypeDesc`, `FieldDesc`, `ErrArenaFull`, `ErrShortValue`, `ErrUnsupported`, `Literal`, `MaxFeatures`, `NewStaticArena`, `UIEvent`, `UIPart`, `UIPrimParam`, `UIPrimitive`, `UITransition`, `UIType`, `UITypeVar`, `ViewVersion`, the `UI*Node` kinds. `Describe` and `TypeDesc` (decision 6, schema formats) come back with the first Protobuf or Avro codec, through `port`.

## Numbers

| Package | Identifiers (with members) | Used outside |
|---|---|---|
| compiler `reasonr` | 787 | 608 |
| engine `port` | 661 | 548 |
| engine `port/porttest` | 72 | 66 |
| engine `registry` | 32 | 23 |
| engine `server` | 2 | 2 |

The identifiers below have no use outside: members of used types (fields and methods that come with the type), values of used enums and the parameter types of used functions; each is below with its reason.

## Compiler: `reasonr`

| Identifier | Members without an outside use | Why it is in the contract |
|---|---|---|
| `API` | `Methods`, `Deprecated`, `Versions` | Returned by `Schema.API`, `APISnapshot`, `APIView` (the external API contract, RFC §31.3.1); the engine writes it as JSON, so its fields are read by `encoding/json`, not by name. |
| `APIDeprecation` | `Method` | The same: an element of `API.Deprecated`. |
| `APIVersion` | `Method`, `Type`, `Date`, `Methods`, `Types` | The same: `API.Versions` and the result of `APISnapshot`. |
| `Access` | — | The type of `MethodInfo.Access`, which the engine reads (the access check of a call). |
| `AccessKind` | `String`, `MarshalText`, `UnmarshalText` | The type of `Access.Kind`; the engine compares it with `AccessActor`, `AccessAny`. The text methods serve JSON. |
| `Arena` | `SetStatic`, `Static`, `Size`, `StringStart`, `StringAppend`, `StringEnd`, `Alloc`, `Unbox`, `Compare` | The one container of values the VM and the engine share: values cross without copying. The VM-only methods come with the type; hiding them needs a wrapper on the hot path of every command. |
| `ArenaRef` | `Off` | The type of `Value.Ref` (a reference into an arena). |
| `Dec` | `Bits`, `IsZero`, `Sign`, `Cmp`, `Neg`, `Abs`, `Add`, `Sub`, `Mul`, `Quo`, `Rem`, `Round`, `Floor`, `Ceil`, `Trunc`, `IsInt`, `Int64`, `Float64`, `Append` | The type of `Value.Decimal()` and `DecFromInt`: the engine passes decimals through; the arithmetic is the VM's. |
| `EntityChange` | — | The element type of `UnitOfWork.Entities`, which the engine commits. |
| `Errors` | `Error`, `JSON` | Model errors come rendered (decision 2): `Error` through the `error` interface, `JSON` for the JSON of AGENTS.md. |
| `FailureKind` | — | The type of a test failure kind; the engine reports `FailFake`, `FailFault`. |
| `Fault` | `Error`, `Unwrap`, `Func`, `PC`, `Op`, `File`, `Line`, `Stack`, `Err` | The VM fault a call returns as an `error`; its fields are the diagnostics the log prints. |
| `Features` | `Empty` | The type of a caller's feature flags; `Empty` is for the compiler's own checks. |
| `FieldInfo` | `Default`, `Extension`, `Inner` | A schema type the engine reads (`Name`, `Type`, `Personal`...); these fields serve the compiler and the VM. |
| `IDStrategy` | `String`, `MarshalText`, `UnmarshalText` | The type of `TypeInfo.ID`; the engine compares it with `IDGiven`, `IDDerived`. |
| `ImpactReport` | `Version`, `Base`, `Violations` | The result of `Impact` (`reasonr impact`): the engine reads the affected tests, the CLI writes the rest as JSON. |
| `JSONCodec` | `DecodeRecord`, `Schema`, `AppendRecord` | The canonical JSON the engine calls directly (decision 6); these methods are the VM's. |
| `JSONError` | `Error`, `Msg` | The error of the canonical JSON: through the `error` interface. |
| `Kind` | `MarshalText`, `UnmarshalText` | The kind of a value; the text methods serve JSON. |
| `Manifest` | `Version`, `App`, `Lang`, `Title`, `Platforms`, `Me`, `Nodes`, `Screens`, `Places`, `Types`, `Messages` | Returned by `Model.Manifest`; the app API writes it as JSON (RFC §31.4). |
| `MethodInfo` | `Extension`, `Doc`, `Destructive`, `Was`, `Inner`, `InnerEvent` | A schema type the engine reads; these fields serve the compiler and tools. |
| `MethodKind` | `String`, `MarshalText`, `UnmarshalText` | The kind of a method; other values of the enum are used. The text methods serve JSON. |
| `MethodRule` | — | A value of `MethodKind` (the enum is used). |
| `MethodValue` | — | A value of `MethodKind`. |
| `Model` | `Migrate`, `ProgramHash`, `Encode` | `Migrate` and `ProgramHash` are called through interfaces (the kernel, the executors of other kinds); `Encode` writes a model file for `reasonr build -o`. |
| `Module` | `Dir` | `Load` returns it; `Dir` is the module directory. |
| `Outcome` | `NewWork`, `Events`, `FieldValues` | The result of `Execute`, which the engine commits; these methods serve the VM and tests. |
| `Phase` | — | The type of `TestOptions.Phase`; the engine uses `PhaseGiven`, `PhaseWhen`. |
| `PhaseAll` | — | A value of `Phase`. |
| `Record` | `Ref` | A value type the engine uses; `Ref` is its arena reference. |
| `RecordCodec` | `FieldDefault`, `Zero`, `Literal` | The binary record codec the engine calls; these methods serve the VM. |
| `Scenario` | — | The element type of `Model.Scenarios`. |
| `ScenarioKind` | — | The type of `Scenario.Kind`; the engine uses `ScenarioDemo`, `ScenarioPerf`. |
| `ScenarioTest` | — | A value of `ScenarioKind`. |
| `Schema` | `TypeText`, `Version`, `Extension` | The schema the engine reads everywhere; `TypeText`, `Version`, `Extension` serve the compiler and the CLI. |
| `ScreenView` | `Version`, `App`, `Screen`, `Lang`, `Platform`, `Title`, `Params`, `Nodes`, `Types`, `Messages` | Returned by `Model.Screen`; the app API writes it as JSON. |
| `StepLog` | — | The element type of `TestResult.Steps`. |
| `Then` | — | A value of `Section` (`Given` is used). |
| `TimerOpKind` | — | The type of `TimerOp.Kind`; the engine uses `TimerSchedule`, `TimerDelete`. |
| `TypeInfo` | `EnumWas`, `Extended`, `Callers` | A schema type the engine reads; these fields serve the compiler and tools. |
| `TypeKind` | `String`, `MarshalText`, `UnmarshalText` | The type of `TypeInfo.Kind`; the engine compares it with `TypeEntity`, `TypeActor`. |
| `TypeValue` | — | A value of `TypeKind`. |
| `UI` | `Type`, `Primitive`, `Screens`, `Primitives`, `Types` | Returned by `Model.UI`; the app API and the web driver walk it, the rest is written as JSON. |
| `UIApp` | `Title`, `Me`, `Auto` | The same: an app of `UI`. |
| `UIBind` | `Result` | The same: a binding of a primitive. |
| `UIFromEvent` | — | A value of the `UIFrom` enum (the web driver uses others). |
| `UIFromForm` | — | A value of the `UIFrom` enum. |
| `UIFromProvided` | — | A value of the `UIFrom` enum. |
| `UINode` | `Kind`, `Primitive`, `Part`, `FieldType`, `Input`, `Text`, `Label`, `Auto`, `Rows`, `Types`, `To`, `Event`, `Back` | A node of a screen: the app API and the web driver walk it, the rest is JSON. |
| `UIParam` | — | The type of `UIScreen.Params`. |
| `UIPrimValue` | `Field`, `Screen` | The type of `UINode.Params`. |
| `UIScreen` | `Title` | A screen of an app: `Title` is written as JSON. |
| `Value` | `Days`, `Date`, `Timestamp`, `Duration`, `Index` | The value type of the boundary; these accessors serve the VM and tests. |
| `When` | — | A value of `Section`. |

## Engine: `port`

| Identifier | Members without an outside use | Why it is in the contract |
|---|---|---|
| `Capabilities` | `FullText` | `FullText`: a store that can search text sets it; ours do not. |
| `Codec` | `ContentType`, `Name`, `NewReader`, `NewWriter` | The codec port (decision 6): a codec module registers with `registry.RegisterCodec`. The built-in `json` and the test `TLV` are the engine's; no codec module exists yet. |
| `Column` | `Field` | The element type of `Table.Keys`; `Field` is the field path an adapter may use, ours use `Name` and `Kind`. |
| `CommandResult` | `Outcome` | `Outcome` is the interpreter result for tests and debugging. |
| `CommittedUnit` | `Projections`, `Forget`, `Keys`, `Contexts` | The value-level unit of work a kind passes through (the working set of a kind with state in memory); these fields are read by the engine's consumers. |
| `CondSet` | `Has` | The type of `Capabilities.Conds` and `AllConds`: the search conditions a store supports; `Has` is the engine's check. |
| `Config` | `Projections` | `Projections` holds the stores of the projections behind `Projection`, which another kind calls. |
| `Consumer` | `Deliver`, `Name` | The type of `EffectsConfig.Consumers`; the engine calls `Name`, `Deliver`. |
| `Counter` | `Add` | Telemetry: a metric of `Metrics`. |
| `DeliverFunc` | — | The type of the `Host.ACL` parameter. |
| `Delivery` | — | The type of `Subscription.C`. |
| `EntityEvents` | `Version`, `Events` | The element type of `CommittedUnit.Entities`. |
| `FeatureResolver` | `Features` | `Features` comes with `FeatureSource`, which the kernel calls. |
| `FeatureScope` | `All`, `String` | The scope of a rollout rule a feature store keeps. |
| `FeatureSource` | — | The type of `KernelConfig.Features`. |
| `Forget` | `Subjects`, `Types` | The type of `Apply` arguments (`MemoryStore`, `RowStore`): the forgotten subjects. |
| `Gauge` | — | Telemetry: a metric of `Metrics`. |
| `Histogram` | `Observe` | Telemetry: a metric of `Metrics`. |
| `KernelConfig` | `Notify` | `Notify`: the host fills it (the bus notifications); a kind passes the configuration on. |
| `LeasedRequest` | `System`, `Name`, `Attributes` | `System`, `Name`, `Attributes` are the ready request (decision 9); Kafka sends `Headers()`, another transport may route by them. |
| `LeasedTimer` | `Tenant`, `Entity`, `Command`, `Args`, `Attempts`, `Arena`, `Offset` | The type `Kernel.Fire` takes and `MemoryStore.Lease` gives: a kind passes it from one to the other. |
| `Limits` | — | The type of `Config.Limits`. |
| `Message` | `Channel`, `Kind`, `Tenant`, `Position`, `Index`, `Type`, `Name`, `At`, `ContentType`, `Attributes` | The ready bus message (decision 9): Kafka uses `Topic`, `Subject`, `Data`, `Headers()`; another bus (NATS, RabbitMQ) may route by `Kind`, `Type`, `Name`. |
| `MessageKind` | `String` | The type of `Message.Kind`. |
| `Metrics` | `Counter`, `Histogram` | Telemetry: a kind gets `Host.Metrics()` and passes it on; the engine makes counters and histograms with it. |
| `Node` | `Delete`, `Line` | The settings node: `Delete` and `Line` serve the engine's checks of engine.yaml. |
| `Order` | — | The type of `Query.Order` an adapter reads. |
| `Table` | `Ordered` | `Ordered`: a table whose rows are read in id order; ours sort by the id column without it. |
| `UniqueKeyViolation` | `Error` | Returned by an adapter as an `error`. |
| `Unit` | `Projections`, `Async`, `Now`, `FiredTimer`, `Idempotency`, `Result`, `Expires`, `Contexts`, `Offset` | The same: the unit a kind commits through `UnitStore.Commit`. |
| `ValueReader` | `Bool`, `Bytes`, `Decimal`, `Duration`, `Enum`, `Field`, `Float`, `Int`, `List`, `Map`, `More`, `Null`, `Object`, `Skip`, `String`, `Time`, `UUID` | The codec port (decision 6): what a codec reads tokens with. |
| `ValueWriter` | `Bool`, `Bytes`, `Decimal`, `Duration`, `End`, `Enum`, `Field`, `Float`, `Int`, `List`, `Map`, `Null`, `Object`, `Reset`, `Result`, `String`, `Time`, `UUID` | The codec port (decision 6): what a codec writes a value with. |

## Engine: `registry`

| Identifier | Members without an outside use | Why it is in the contract |
|---|---|---|
| `Command` | — | The type of the `RegisterCommand` parameter. |
| `Deps` | `Metrics` | `Metrics`: an adapter may report metrics (ours log). |
| `HubFactory` | — | The type of the `RegisterHub` parameter. |
| `IntegrationFactory` | — | The type of the `RegisterIntegration` parameter. |
| `KMSFactory` | — | The type of the `RegisterKMS` parameter. |
| `PublisherFactory` | — | The type of the `RegisterPublisher` parameter. |
| `RegisterCodec` | — | The codec port (decision 6): a codec module registers with it; none exists yet. |
| `StoreFactory` | — | The type of the `RegisterStore` parameter. |
| `StoreTrait` | — | The type of `PersonalData`, which postgres passes. |

## Engine: `port/porttest`

| Identifier | Members without an outside use | Why it is in the contract |
|---|---|---|
| `Buffer` | `Write` | `Write`: through `io.Writer` (the server log of a test). |
| `Factory` | — | The parameter type of `RunUnitStore`, `RunRequests`, `RunPersonal`, `RunEntitySink`, `RunStateTransfer`. |
| `HubFactory` | — | The parameter type of `RunHub`. |
| `ProjectionFactory` | — | The parameter type of `RunProjectionStore`. |
| `PublisherFactory` | — | The parameter type of `RunPublisher`. |
| `Ready` | — | The pilot platform's webhook test (`pilot/platform/src/engine/webhook`, its own workspace, outside this audit). |

## Engine: `server`

`Main` (`reasonrd`, the pilot platform, the web driver's test server) and `Run` (the tests of the engine kinds and of the distribution) are used.

## Open questions

- The codec port (`Codec`, `ValueReader`, `ValueWriter`, `RegisterCodec`) is decision 6, but no codec module exists: keep it as the extension point, or hide it until the first Protobuf or Avro codec?
- `Arena` carries the VM's methods (`Alloc`, `SetStatic`...): hiding them needs a wrapper type on the hot path.
- `Unit`, `CommittedUnit`, `LeasedTimer` are the value-level kind contract: the fields the engine reads are visible to kinds that only pass them through.
