# pytest-sample

Generic Python fixture for the manual test-generation criterion run
(≥80% of targets pass within ≤2 repairs). Requires `tu-agent` on PATH,
a configured provider, and pytest installed.

    cd scripts/fixtures/pytest-sample
    ../../testgen_criterion.sh 3

No hand-written tests on purpose: every public function is a gap.
