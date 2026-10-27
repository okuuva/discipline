# Discipline PR delivery

Implement the functional contract in [the CD-ripping plan](cd-ripping-tool.md) as five reviewable changes.
Each change includes its own tests and documentation.
Do not defer safety checks to the final integration PR.

| PR | Scope and acceptance | Implementation dependencies |
| --- | --- | --- |
| 1 | Local SQLite records, exclusive phase claims, durable recovery request CLI, pinned Go/Nix package checks; retain requests/receipts across restart and fail closed on state loss | None |
| 2 | One-drive libudev lifecycle, insertion identity and isolated patched-abcde extraction; deduplicate events/restarts, reject unsupported media and require processing evidence before review | 1 |
| 3 | Bounded image processing and authenticated Telegram review; job-specific, revisioned, replay-safe decisions and verified embedded/sidecar cover | 1, 2 |
| 4 | Separate mount-safe NFS publisher; source/destination verification, newer-rip protection, uncertain-copy reconciliation and exclusive local cleanup | 1, 3 |
| 5 | NixOS service integration, isolated Linux service tests and runbook; prove process isolation and document guarded manual import/rollout | 2, 3, 4 |

The GitHub stack is linear in this order even where a later change has no direct code dependency on every earlier change.
A locally validated and reviewed parent satisfies an implementation dependency.
Publication alone does not satisfy validation, merge or deployment gates.
New PRs are drafts; only a human may mark them ready.

The foundation's CLI records requests but does not run workers.
Request processing arrives with the owning extraction/review/publisher process.
The intermediate packages are not deployable replacements for the existing ripper.
Do not enable any service until the final integration checks pass.

## Gates

Each PR requires formatting, relevant Go tests with the race detector, its Linux Nix package checks and both code-review passes.
The final service check additionally requires Linux virtualization suitable for an isolated NixOS test.
CGO/libudev lifecycle checks arrive with PR 2, not a macOS substitute.

Merge of a deployable application requires the complete stack's isolated package/service checks.
Host activation requires separate approval, credentials, device permissions, disabling competing drive handlers, hardware/media parity checks and a disposable destination.
No application PR may change the sibling host configuration, live beets catalog, existing mount or managed library.
Real Telegram messages and shared-storage probes remain separately authorized work.
