# Bank

The bank model from the RFC (§26): customers and accounts, a transfer that collects all errors, the external system Antifraud with its fake (`src/contexts/Accounts/antifraud.fake.rr`), the customer app on primitives and acceptance tests. Sources follow the project parts (RFC §23): context `src/contexts/Accounts/`, app `src/apps/Bank/`, primitives `src/primitives/`. Rules for working with a model are in [AGENTS.md](../../AGENTS.md).

The recipient account number is entered with an own primitive `AccountNumber` (`src/primitives/primitives.rr`): the model declares it, UI developers implement it. This example has no implementation, so the automatic UI is drawn in its place.

The model runs on the standard engine, `reasonrd` from PATH (`reasonr.yaml`):

```sh
reasonr fmt -w && reasonr check         # sources from reasonr.yaml; the base model and all extensions
reasonr test                            # acceptance tests in memory
reasonr test src/contexts/Accounts/bank.test.rr:80   # one test: the one containing the line
reasonr dev                             # http://localhost:8080/ui/ — test mode in memory, the Antifraud fake
```
