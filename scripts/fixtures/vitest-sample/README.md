# vitest-sample

Generic TypeScript fixture for the manual test-generation criterion run
(≥80% of targets pass within ≤2 repairs). Requires `tu-agent` on PATH,
a configured provider, Node.js, and `npm install` run once in this
directory (installs vitest).

    cd scripts/fixtures/vitest-sample
    npm install
    ../../testgen_criterion.sh 3

No hand-written tests on purpose: every public function is a gap.
