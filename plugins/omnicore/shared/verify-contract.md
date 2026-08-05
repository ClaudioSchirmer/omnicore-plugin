# shared/verify-contract.md — the reconcile rule every Final Verify obeys

One rule, owned here so every mutating skill's gate enforces it identically (the
pattern is `implement`'s "the plan's own verify step, executed" — generalized).

**After the skill's fixed verify levels pass, REOPEN the run's own spec/plan and walk
its stated promises ITEM BY ITEM.** For each promise — a coverage target, a rule to be
tested, a surface to be exercised, a knob to be observable, a boot to be proven:

1. **Evidence, not assertion.** Each item is checked with a real command whose output
   is shown (the failing case would be visible in it). "Should work" and a green
   summary are not evidence.
2. **An unmet stated target is RED.** Fix until met, or surface it to the dev as an
   explicit, named deviation they must accept — NEVER fold a miss into a green
   summary. A verify that passes with a broken promise inside is a false report.
3. **Measure the way the convention defines.** A number proved the wrong way (package
   coverage where the convention says per-file; a probe of the wrong profile) counts
   as unmet — show the measurement itself, not just the number.

Deviations the dev accepted are listed in the final report under their own heading —
visible, never silent.
