# Taxi

The taxi model from the RFC (§25): contexts `Ordering` (orders and customers) and `Dispatch` (driver matching and the trip) linked by ACL (`*.acl.rr`, RFC §23.2), apps and screens on primitives, texts, acceptance tests, fakes of external systems (`*.fake.rr`), demo and performance scenarios, and the `acme` extension. Sources follow the project parts (RFC §23): `src/contexts/`, `src/apps/`, `src/extensions/acme/`. Rules for working with a model are in [AGENTS.md](../../AGENTS.md).

The model requires the authentication module ([reasonr-auth](https://github.com/tycheg/reasonr-auth)) and runs on the standard engine, `reasonrd` from PATH (`reasonr.yaml`). A full project built on it — the model with its own platform, UI and integrations, split by roles — is the pilot ([pilot-model](https://github.com/tycheg/pilot-model), [pilot-integrations](https://github.com/tycheg/pilot-integrations), [pilot-ui](https://github.com/tycheg/pilot-ui), [pilot-platform](https://github.com/tycheg/pilot-platform)).

```sh
reasonr fmt -w && reasonr check         # sources from reasonr.yaml; the base model and all extensions
reasonr test                            # acceptance tests in memory
reasonr test src/contexts/Ordering/order.test.rr:25   # one test: the one containing the line
reasonr test --driver api,web           # through the API and through UI places (the browser: reasonrd web install)
reasonr dev                             # http://localhost:8080/ui/ — test mode in memory, fakes
```
