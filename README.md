# discipline

A Linux CD-ripping application being built around patched abcde.
The intended flow is local extraction, Telegram artwork/ownership review, then verified delivery to the existing beets inbox.
Final library import stays manual.

## Development status

This first change provides local job storage, recovery request commands and the Go/Nix build.
It does not yet run extraction, Telegram or publication workers.
Queued requests remain queued until their owning worker is implemented.
Do not deploy this intermediate package as a working ripper.

See the [delivery plan](docs/plans/pr-stack.md) for the five PRs and the [functional contract](docs/plans/cd-ripping-tool.md) for safety and rollout requirements.

## Build and check

On Linux:

```bash
nix flake check
nix build .#discipline
nix develop
go test -race ./...
```

The package check also runs gofmt validation and go vet.
Nix pins the toolchain and dependency closure; CGO supplies the SQLite driver.
No host-installed media tools or interactive shell configuration are used by this foundation.

## Local state commands

```bash
discipline jobs
discipline show JOB_UUID
discipline requests
discipline retry JOB_UUID
discipline cleanup JOB_UUID
discipline new-insertion
discipline reconcile JOB_UUID --outcome not-published --note 'evidence from inspection' --confirm
```

Global `--state` and `--spool` flags must precede the command.
Defaults are `/var/lib/discipline` and `/srv/abcde`.
Both must be separate, private local directories owned by the service user.
The CLI will not initialize missing state.

Commands return JSON.
`jobs` and `show` expose phase, revision, last error, cleanup status and permitted recovery actions.
`requests` distinguishes queued requests from completed work.
Mutating commands record intent, never change a job's phase or perform filesystem cleanup themselves.
Conflicting requests are rejected while an earlier request is queued.
Workers must atomically recheck the recorded revision and their file ownership before executing a request.

A retry never converts an uncertain copy into a new extraction.
Reconciliation requires a reason and explicit confirmation.
Before reconciling an uncertain copy, quiesce its publisher and the manual importer and check whether the audio has already been consumed or superseded.
Missing inbox files do not prove that another copy is safe.
A published receipt cannot be abandoned or reset to an unpublished state.

## State protection

SQLite uses WAL, FULL synchronous commits and short transactions.
A monotonic job sequence distinguishes creation order; each attempt has its own UUID.
Revision-checked claims prevent two workers from acquiring the same phase.
No transaction may span subprocess, network or NFS work.

Initialization is allowed only for empty local state/spool directories.
A retained initialization sentinel detects database loss even after all audio is cleaned.
Missing, corrupt, incomplete or unsupported state and orphan spool entries fail closed.
Do not delete the sentinel to make startup succeed or reconstruct jobs from directories.
Preserve the state directory, SQLite sidecars and spool for operator reconciliation.
Restoring an older database also requires reconciliation with any completed transfers/imports.
The beets backup does not back up discipline state.

Filesystem configuration and private ancestor directories are trusted administrative inputs.
The application does not protect against another process running as the same service user rewriting its state.
No command in this foundation reads NFS, contacts Telegram or opens the beets database.
