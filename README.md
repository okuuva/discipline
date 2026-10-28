# discipline

A Linux CD-ripping application being built around patched abcde.
The intended flow is local extraction, Telegram artwork/ownership review, then verified delivery to the existing beets inbox.
Final library import stays manual.

## Development status

The package now provides local job storage, a single-drive extraction service and recovery requests.
Telegram review, publication and NixOS activation follow in dependent PRs.
Artwork retrieval stays disabled until the bounded review pipeline is installed.
Do not deploy this intermediate package as the complete ripper.

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
Nix pins the toolchain, libudev/SQLite CGO dependencies and media executables.
Tests start and drain a real libudev monitor without accessing optical hardware.
Pinned-abcde fixtures use a synthetic WAV and stubbed disc reader, but real LAME, eyeD3 and rsgain.
They check unknown-disc extraction, processing evidence, failed gain, single-track identification and metadata quoting.
No host-installed media tools or interactive shell configuration are used.

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
Mutating CLI commands record intent, never change a job's phase or perform filesystem cleanup themselves.
The running extraction service processes rip retries and explicit new-insertion requests.
Other requests wait for their owning workers in later PRs.
A completed retry request means the worker accepted the retry, not that audio processing succeeded.
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
No command in this package reads NFS, contacts Telegram or opens the beets database.

## Extraction service

`discipline --drive /dev/sr0 serve` runs the one-drive service in the foreground.
This command accesses real hardware; run it only during an authorized hardware check with competing drive handlers stopped.
It initializes only empty private local state/spool directories and takes a lifetime local drive lock.
Its media subprocesses inherit the lock.
The packaged abcde configuration gives each attempt its own HOME, temporary directory, scratch and output directories.
MusicBrainz lookups have a deadline; absent metadata produces an unknown-disc rip rather than a prompt.

The service subscribes to libudev before taking a stable startup sample.
Repeated events and restarts with the same loaded disc reuse the recorded insertion.
Observed removal/reinsertion creates a new attempt, even for the same TOC.
Offline removal/reinsertion of the same disc requires `new-insertion` because it cannot be observed retrospectively.
Data-only media is ignored; mixed-mode CDs are held as unsupported.
A TOC fingerprint describes a layout, not the physical disc or a hash of its audio.

Extraction never automatically retries a failure.
Explicit retry requires the matching disc and preserves that attempt's scratch/output.
The service requires track-level read/encode/tag/move evidence, successful gain, no recorded errors and decodable MP3s before marking audio ready for review.
A zero abcde exit status is not enough.
Successful processing ejects only the same observed insertion; failures retain it and the local output.
The worker pins the drive descriptor and locks the tray before claiming an extraction.
Its `BLKGETDISKSEQ` media generation must match the reconciled libudev observation, even if the replacement CD has an identical TOC.
The tray remains locked through processing; ejection uses that pinned descriptor and the same insertion identity.
Devices without the required tray-lock and disk-generation ioctls fail closed and require a hardware compatibility decision before deployment.

Shutdown cancels the media process group and waits for its children to exit before releasing file ownership.
An uninterruptible device operation can delay shutdown; do not bypass the lock to start a competing extractor.
The pinned backend retains the homeslab single-track fix and additionally quotes metadata fields that abcde evaluates as shell assignments.
Real device permissions, ejection, drive calibration, media parity and playback remain rollout checks, not claims established by synthetic fixtures.
