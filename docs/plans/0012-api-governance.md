# Plan: governance of the external API — a stable contract

| | |
|---|---|
| **Basis** | [Roadmap](../roadmap.md), item 4. The ACL moved to item 3 ([plan 0011](0011-contexts-acl-aggregates.md)) |
| **Status** | Accepted 2026-11-12: the decisions of §3 and the proposals in the questions of §5. G1–G4 done. Done |
| **Branch** | `api-governance` |
| **Note** | Paths and RFC section numbers are those of the time (the repository `tycheg/reasonr` before the split) |

---

## 1. Goal

A system in ReasonR has an external API. Integration engineers and other teams use it, and they are not released together with the model, while the model changes every day. The tool must:

- keep the **published contract stable**: a model change that breaks clients does not pass `reasonr check`;
- let the model evolve **with almost no code** from the model's author: no adapters, DTOs or copies of methods;
- show **what is published, in which versions and who uses what**, so that removal is safe.

The interface (the app API `/app/…`) is not part of the contract: it is released together with the model.

## 2. How Stripe does it

- **A version is a date:** `2024-06-20`. Since 2024 the date gets a release name, for example `2024-09-30.acacia`. Breaking changes come out in named releases twice a year, compatible ones monthly.
- **An account is pinned to the version** of its first request. A request may name another version with the `Stripe-Version` header. Webhooks come in the account's version.
- **There is one implementation — the latest.** A chain of "version change modules" serves the old versions: every breaking change is described by a module that translates the response (and the request) one version back. Stripe's developers write these modules.
- Compatible changes (a new method, a field, an optional parameter) are visible to every version right away.

We can **derive** the translations between versions **from the model** instead of writing them by hand. `was` describes a rename. A new parameter gets a default value. What is removed is not removed while a supported version sees it.

## 3. Decisions (proposal)

| # | Decision |
|---|---|
| 1 | **Nothing is published by default.** The external `/api` in production serves only published methods. Test mode (`--test`, `reasonrd dev`, `--driver api`) serves every public one, as now |
| 2 | **Publishing needs no code in the model.** `reasonr api publish Ordering.Order.Cancel` (or a pattern `Ordering.Order.*`) adds a method to the contract `api/contract.json`. The contract gets the method's parameters, results, errors, enums and notifications, and the types they reach. The file is committed: an API change is reviewed as a diff of this file. The compiler reads it and tells the engine which methods are open |
| 3 | **Versions by date, as at Stripe.** `reasonr api release` freezes the current contract in `api/versions/2026-11-12.json`. A client is pinned to its API key's version or names it with the `Reasonr-Version` header. There is one implementation — the current model. The engine serves the old versions with translations derived from the model (decision 5) |
| 4 | **The check is in `reasonr check`.** If the module has `api/`, `check` compares the model with every supported version. A change that cannot be derived as a translation is an E11xx error with a fix. The existing data compatibility comparer (`compat`) is used: the API rules are added to it. There is no separate CI step |
| 5 | **Translations without code.** `was` extends from entity fields to methods, parameters, value object fields and enum values: `CancelOrder(cancelReason string was reason) was Cancel`. A client of the old version calls `Cancel` with `reason` and gets a response with the old names. A new parameter with a default value is not visible to the old version. A new required parameter without a default value, narrowing a type and changing access are errors: a new method next to the old one is needed |
| 6 | **Removal goes through deprecation.** `reasonr api deprecate Ordering.Order.Card --sunset 2027-03-01` writes a mark into the contract; the model does not change. The deprecated item gets `deprecated` in OpenAPI and the headers `Deprecation` and `Sunset` in responses. A method or a field can be removed from the model once the date has passed or no supported version contains it. Otherwise — an E11xx error. `reasonr api retire 2026-11-12` drops the support of an old version |
| 7 | **Usage.** The engine counts calls by method, version and tenant (the metric `reasonr_api_calls_total`, `reasonrd api usage`): one can see who is on an old version and whether it can be dropped |
| 8 | **Publishing the description.** `reasonr api openapi [--version date]` builds OpenAPI 3.1 from the contract without the engine: `##` descriptions, examples from tests (the arguments of `when` steps), `deprecated`. A portal and SDKs — standard OpenAPI tools (Redoc, openapi-generator); we build none of our own |

## 4. Example

```sh
reasonr api publish 'Ordering.Order.*'      # the contract: the order's public methods
reasonr api release                         # version 2026-11-12
```

A month later a parameter is renamed — one edit in the model:

```rr
## Canceling an order by the customer or support
command(customer, support) Cancel(cancelReason string was reason)
  ...
```

```text
$ reasonr check                              # without was
order.rr:120:3: E1104 API version 2026-11-12: parameter reason of command Ordering.Order.Cancel is removed — clients of this version will get an error
  fix: cancelReason string was reason
```

## 5. Questions to the author

| Question | Proposal |
|---|---|
| Versions by date with translations from the model (§2, decision 3) or one compatibly evolving version? | By date: clients of an old version see the old names, new ones only the new names; no code is written for this |
| Publishing — a command `reasonr api publish` that writes a contract file, or a mark in the model (`## @api`)? | A command and a file: the model does not change, and the decision "publish" is visible in review as a separate file |
| How does a client choose a version: the API key, a header or a path `/api/2026-11-12/…`? | The API key by default and the `Reasonr-Version` header, as at Stripe; the path does not change |
| The contract of a tenant extension (its own public methods)? | A separate directory `api/<extension>/`, the same rules |

## 6. Stages

| Stage | Result |
|---|---|
| **G1. The contract** | `reasonr api publish\|release`, `api/contract.json` and `api/versions/`, the engine opens only what is published, `reasonr api openapi` |
| **G2. The check and translations** | API rules in `compat` (E11xx), `was` on methods, parameters, value fields, enums; the engine serves old versions with translations |
| **G3. Deprecation and usage** | `reasonr api deprecate\|retire`, the headers `Deprecation`/`Sunset`, the ban on removal, metrics and `reasonrd api usage` |
| **G4. Documents** | The README (the process of changing the API), AGENTS.md, the RFC; taxi and bank with a published API |

The acceptance criterion:
- `reasonr check` catches removing a parameter of a published taxi command, and with `was` the check passes;
- a client of version 2026-11-12 (a test through `--driver api` with the version header) works with the old name after a rename;
- an unpublished method is not available in production;
- the taxi and bank OpenAPI passes an OpenAPI 3.1 validator.

## 7. Progress

| Stage | Status | What was done |
|---|---|---|
| G1. The contract | Done | `api/contract.json`, `api/versions/<date>.json`; `reasonr api publish\|unpublish\|release\|list`; the contract is in the schema (`Schema.API`); outside test mode `/api` and `/openapi.json` serve only what is published (and the response commands of external systems); `reasonr api openapi [--version]` — from the contract without the engine (OpenAPI moved to `compiler/apidoc`) |
| G2. The check and translations | Done | `was` on commands, queries, parameters, enum values (fields had it); `reasonr check` compares the model with the released versions: E1104 (with a `was` hint), E1105, E1106, E1107; the engine serves the version from `Reasonr-Version` with the schema as that version sees it (`apicontract.View`): the same numbers, the old names |
| G3. Deprecation and usage | Done | `reasonr api deprecate --sunset`, `reasonr api retire`; the headers `Deprecation`, `Sunset`, `deprecated` in OpenAPI; removing a deprecated item after `sunset` is not an error; the metric `reasonr_api_calls_total{method, version}`, `reasonrd api usage --url` |
| G4. Documents | Done | RFC §31.3.1, the tools of §33, AGENTS.md, the README; taxi and bank with a contract and version 2026-11-13; an engine test: an unpublished method is 404, a client of a version passes the old parameter name, a deprecated one comes with the headers |

**Deviations** (for the author's decision):

1. **The version is only a header** `Reasonr-Version`; without the header — the current model. There is no pinning of a version to an API key (as at Stripe): authentication issues API keys, not the engine.
2. **The response commands of external systems** (`command(matching) DriverFound`) are always open: they are the integration service's contract (`/openapi/integrations.json`), not the external API's.
3. **Translations** cover the names of methods, parameters, value object fields and enum values. Errors and notifications are not renamed; new enum values in results are not hidden from old clients.
4. **The contract of an extension** (`api/<extension>/`) is not done: every tenant model opens the methods of the base contract.
5. **`reasonrd api usage`** reads the metrics of one instance (`/metrics`), without a breakdown by tenant; the cluster summary is in Prometheus.
6. There is no check of the OpenAPI by an external 3.1 validator — the description is built by the same code as `/openapi.json`.
