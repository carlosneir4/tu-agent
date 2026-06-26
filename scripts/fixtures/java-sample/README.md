# java-sample — generic fixture for the Java-readiness check

A minimal, fictional Java tree used by `scripts/java_ready_check.sh` to exercise
the deterministic, no-model-call tu-agent path the way a real Java repo would.

It is deliberately constructed to trigger the semantic extractor features that
make tu-agent useful on Java:

- `OrderService` **overrides** `BaseService.describe()` → an `overrides` edge.
  (The extractor detects overrides via the symbol table, not via the `@Override`
  annotation — the annotation is Java compiler sugar and ignored by the extractor.)
- `CatalogRepository` extends `com.external.orm.AbstractRepository`, a class with
  no source in the tree → an `external::` stub node.

Two packages under `com.acme.shop` (`orders`, `catalog`) give the concept index
two concepts to map. All names are fictional (CLAUDE.md §9).
