# discipline

A Linux CD-ripping application being built around patched abcde.
The intended flow is local extraction, Telegram artwork/ownership review, then verified delivery to the existing beets inbox.
Final library import stays manual.

## Development status

The package now provides local job storage, extraction, authenticated Telegram artwork review and local finalization.
A separate publisher now verifies and transfers approved jobs into the classified NFS inbox.
NixOS service integration follows in the final PR.
A `ready` job is finalized locally; only `published` means its transfer was verified.
Do not deploy this intermediate package as the complete ripper.

See the [delivery plan](docs/plans/pr-stack.md) for the five PRs and the [functional contract](docs/plans/cd-ripping-tool.md) for safety and rollout requirements.

## Build and check

On Linux:

```bash
nix flake check
nix build .#discipline
nix develop
```

The package check runs gofmt validation, go vet and race-enabled Go tests with a two-minute test deadline.
Use that check for the complete pinned media and image-policy fixtures; the development shell is for editing.
Nix pins the toolchain, libudev/SQLite CGO dependencies and media executables.
Tests start and drain a real libudev monitor without accessing optical hardware.
Pinned-abcde fixtures use a synthetic WAV and stubbed disc reader, but real LAME, eyeD3 and rsgain.
They check unknown-disc extraction, processing evidence, failed gain, single-track identification and metadata quoting.
Local HTTP fixtures cover URL pinning/redirects, Telegram authorization, replay, notification failure and token redaction.
Real ImageMagick, eyeD3 and FFmpeg checks verify replacement of wrong embedded/sidecar art and unchanged compressed MP3 packets after gain and cover tagging.
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
`jobs` lists phase, revision, last error, cleanup status and permitted recovery actions.
`show` returns a `job`, its `local_path`, and its `review` when available, including the candidate, frozen category and media manifest.
`requests` distinguishes queued requests from completed work.
Mutating CLI commands record intent, never change a job's phase or perform filesystem cleanup themselves.
The main service processes rip retries, finalization retries and explicit new-insertion requests.
The separate publisher processes copy retries, reconciliation and cleanup requests.
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
Local status/recovery commands do not read NFS, contact Telegram or open the beets database.
The foreground service contacts Telegram and metadata/artwork providers, but never sends audio.

## Extraction service

`discipline --drive /dev/sr0 --telegram-chat CHAT_ID --telegram-user USER_ID --telegram-token-file /path/to/private/token serve` runs the one-drive service in the foreground.
Repeat `--telegram-user` for each allowed numeric user ID.
With systemd credentials, the token path defaults to `$CREDENTIALS_DIRECTORY/telegram-token`.
The token file must be private, regular and outside the Nix store; the service refuses to run as root.
This command accesses hardware and sends real bot messages; run it only during an authorized check with competing drive handlers stopped.
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

## Artwork and Telegram review

The coordinator uses the selected MusicBrainz release in retained abcde metadata to try Cover Art Archive through the bounded downloader.
Abcde's own artwork fetching and embedding remain disabled so another fetcher cannot bypass those limits.
Missing artwork produces a text request rather than a default image or category.
Final metadata/release selection remains manual in beets, not in this bot.

A preview says "ripped, awaiting review", identifies the job and links the public source when known.
An uploaded image is explicitly labeled as having no public source URL.
Covers beyond Telegram's photo aspect-ratio limit arrive as JPEG documents with the same approval buttons; open the attachment before approving.
Reply directly to that preview with an HTTPS URL, a photo or a JPEG/PNG/WebP file.
Send the image as a file when Telegram's photo resizing is unwanted.
"Approve Owned" or "Approve Loaned" freezes the displayed image revision and category together.
Only the configured chat and allowed senders may change a job.
Old previews, duplicate updates and edits after approval cannot replace the chosen image or change its category.

Input is limited to 10 MiB, one image and 25 megapixels.
Downloads and image processing each have a 30-second deadline.
URLs must use HTTPS without credentials; all resolved addresses and up to three redirects are checked, then curl pins the validated address with proxies and curl configuration disabled.
Cover servers must support HTTP/1.1; chunked responses and interim headers are supported.
Telegram file URLs are built separately and never stored as source links or returned in errors.
ImageMagick runs with a private configuration environment, no delegates, only JPEG/PNG/WebP coders, a 512 MiB memory limit and no disk/map cache.
Conversion strips metadata, applies the existing 4:2:0/progressive-JPEG settings and fits within 1000×1000.
A failed replacement leaves the previous candidate and its revision intact.

The update effect, applied-event receipt and polling cursor commit together.
Receipts identify callback queries or chat/message pairs, not transport update numbers: [Telegram can randomize update IDs after a week idle](https://core.telegram.org/bots/api#update).
After 24 hours without a received update, polling starts from the earliest unconfirmed update instead of sending the old offset.
Telegram itself retains incoming updates for at most 24 hours; after longer outages, resend unanswered actions for jobs still awaiting review.
A send failure leaves notification pending with a per-job retry delay; it cannot prevent incoming actions or later previews.
A failed replacement schedules the previous preview again with a rejection notice, so an API outage cannot silently lose that notice.
An ambiguous delivery can produce a duplicate preview, not duplicate approval effects.
Drive handling, review and finalization run independently, without database transactions spanning network or subprocess work.
Image work within review stays ordered: one download and conversion can delay later review actions for up to 60 seconds, while extraction and finalization continue.
On restart, interrupted cover work returns to review with the previous candidate; interrupted finalization requires `retry`.

Finalization tags a temporary MP3 copy, verifies its embedded image and unchanged compressed audio packets, then atomically replaces the original.
A failed or interrupted tag write cannot become a retry's new audio baseline.
It writes and verifies the same approved JPEG once per album as `cover.jpg`, then freezes the complete media manifest.
Partial finalization retains local output and the frozen approval for an explicit, repeat-safe retry.
No later automatic fetch can replace that image.

## Publisher and local cleanup

Run `discipline --media-source 'spikenology.localdomain:/volume2/Media' publish` as the same non-root user, separately from `serve`.
The main service initializes empty local state first; the publisher refuses to create missing state.
The defaults are `--media-mount /mnt/media` and `--storage-ready /run/beets-storage-ready`.
These must describe the existing storage contract, not a local fallback directory.
The publisher requires a regular readiness marker and the exact visible writable NFS source/target with `hard,sec=sys`.
It pins directory descriptors, checks their kernel mount IDs, rejects nested mounts, symlinks, shared hard links and special files, and passes pinned source/destination descriptors to rsync.
If the mount disappears or is replaced, copying cannot fall through to the uncovered local mountpoint.
Linux `openat2` and `statx` mount IDs are required; unsupported kernels fail closed.

Only the frozen `media/` tree is copied into `Music/incoming/<Owned|Loaned>/<TOC-SHA256>/` below that mount.
The source must match the nonempty frozen manifest before destination changes.
Rsync uses checksums, `--delete-delay` and file fsync, then discipline checks the exact destination file set/hashes and flushes files/directories before recording `published`.
An approved rerip may replace regular files and remove stale contents only within its exact category/disc directory.
The highest creation sequence ever approved for that destination prevents a late older approval or retry from overwriting a newer approved rip, even after the newer attempt is cleaned or abandoned.
Older attempts become `superseded`, retain local data, and may be explicitly abandoned and cleaned.

Publication is not an atomic album swap and is not acceptance into the managed library.
Keep the importer and other writers out of that disc directory while copying or retrying.
For a category-wide import, wait for all approved transfers in that category to complete and do not approve/retry more until the import ends.
A failed copy leaves a partial inbox, retains local output and enters `copy-failed`; `retry` repairs that same copy without reripping.
A crashed publisher leaves `uncertain`, including a crash after verification but before its SQLite receipt.
Quiesce the publisher and importer, check for prior consumption or newer rips, then enqueue an audited `reconcile` decision.
A missing inbox alone never authorizes resending.
Completed and superseded jobs never automatically resend.

The main service retries transfer-complete or not-ready Telegram notices from local state, without reading NFS.
Automatic cleanup waits for the transfer-complete notice to succeed and removes only the published attempt, keeping its receipt.
`--keep-local` disables automatic cleanup; explicit `cleanup UUID` still works for idle published or explicitly abandoned jobs.
A cleanup failure retains status and requires an explicit cleanup request rather than a busy retry loop.
An interrupted cleanup resumes from its exclusive `cleaning` claim; an already absent attempt is safe only under that claim.
Nothing cleans the shared inbox, managed library or another local attempt.

The publisher holds a local lifetime lock inherited by rsync and waits for subprocess quiescence, including cancellation.
Hard-NFS I/O can block indefinitely; configure its service with `TimeoutStopSec=infinity` and never force a competing publisher, unmount, soft-mount retry or source cleanup to bypass it.
Restarting `serve` cannot release the publisher's lock or requeue its unresolved copy.
Local status, extraction and review remain independent of blocked NFS work.
Actual NFS and power-loss behavior still require separately authorized rollout tests.
