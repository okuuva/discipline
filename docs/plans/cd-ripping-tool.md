# discipline: standalone CD-ripping application

## Status and intent

Discovery and planning only, focused on ripper replacement after [Q9's scope decision](cd-ripping-tool.questions.md#round-4-resolution).
Q4–Q7, Q9–Q15 and Q17–Q18 are answered and preserved verbatim in the questions file.
Q8 is superseded; the user now reports beets running on homeslab with NFS-mounted music.
The corresponding [Nix implementation and runbook](../../../nix-config/hosts/nixos/homeslab/media/music/README.md) were inspected read-only.
Library-management relocation is completed external work, not a future migration this ripper change must implement.
Q12 confirms two-way Telegram review and the existing terminal beets import after considering web importers.
Browser import integration is deferred, not a second interface to implement.
Q13 selects a standalone application around patched `abcde`, named `discipline`, in a dedicated repository.
Q14 confirms `/Users/oula/gits/discipline` as the implementation workspace.
The user subsequently authorized creating the jj repository and moving the planning documents here; that relocation is complete.
The Q15 retry completed on 2026-10-27: the NFS mount rejected no-replace publication to an absent destination with `EINVAL`; all probe objects were removed and cleanup was verified.
The user subsequently clarified that the inbox is disposable and overwriting files for a rerip is acceptable.
The no-overwrite/atomic-album-visibility requirements were introduced by this plan, not required by the existing workflow.
Q16's reserved-directory proposal and further probe are superseded; neither is needed for the clarified direct-copy workflow.
[Q17](cd-ripping-tool.questions.md#round-11-resolution) accepts replacing a disc's inbox contents, including removing stale files within that exact directory.
[Q18](cd-ripping-tool.questions.md#round-12-resolution) gives Go as the user's preferred language; the proposed layout and tooling below now use Go.
The user also asks about a future beets import WebUI; separate application ownership is recommended below, without adding that future work to this change.
Repository initialization and document relocation were authorized; implementation, installation, database writes, publication and deployment remain unauthorized.
These planning documents now live in `/Users/oula/gits/discipline`, alongside the future implementation.
Do not invoke `/implement` against this draft.
Plan exactly one discipline application/package change; do not include host activation or dotfiles migration commits.
The existing beets/NFS setup is the integration baseline, not an implicitly authorized second change.

Replace shell-owned orchestration with discipline, a standalone NixOS application using existing extraction and media-processing tools.
Keep the user's existing beets import workflow, make cover approval/replacement easy, and carry owned/loaner classification to import without tying it to extraction.
Preserve the audio output and safe transfer/cleanup behavior.
Do not build a unified UI or relocate the music files merely to automate the optical drive.
Do not relocate beets, change the NFS transport, or reimplement catalog locking/backups as part of this scope.

## Decisions from the user

- Q4: several interfaces are acceptable; the existing interactive beets import works well.
  Previously the user checked the cover URL in `abcde` and supplied a better URL when needed.
  Preserve that capability, not necessarily the old prompt sequence.
- Q5: extraction should start without waiting for human input, followed by ownership/metadata/art review before delivery.
  The answer selected wording that put review before rsync.
  Beets now runs on homeslab, so no NAS-shell adapter or second catalog is needed.
  Q11 accepts publishing an ownership/cover-reviewed album to the inbox before final metadata matching and library acceptance.
- Q6: owned/loaner is an import classification.
  Beets already tags albums and places them in separate directories; pre-divided rip output has just been a convenient input to that process.
  Loaner-to-owned reclassification belongs to the user and beets and is out of scope.
- Q7: separate interfaces are fine for the first implementation; minimize custom code and operational maintenance.
  A unified interface may be considered later, without rebuilding existing capabilities.
- Q9: keep this plan focused on the ripper and handle beets/storage separately in `nix-config`.
  The user reports that migration has now happened; preserve the inspected implementation rather than planning it again.
- Q13 A: build a standalone application around patched `abcde`, with its own Nix package/module and focused checks, outside dotfiles and host configuration.
  The user named it `discipline`: "Let's call it discipline because it meticulously rips your discs."
  ARM integration is no longer an implementation branch.
  Q14 confirms `/Users/oula/gits/discipline` as the future workspace, without authorizing its creation.
- After Q16, the user clarified: extract on the server's local disk, then transfer directly to the beets inbox; overwriting inbox files, including intentional rerips, is acceptable.
  The inbox is temporary working storage, not a protected library or backup.
  Use isolated local attempts and a sufficiently unique disc-specific inbox path; `/srv/abcde` is the proposed local staging root.
  Do not add NAS-side staging, directory reservations or no-clobber publication to protect an invariant the user does not require.
- Q18 prefers Go for the coordinator and raises the future import WebUI's ownership.
  Go replaces the assistant's unapproved Python proposal.
  The future WebUI remains deferred under Q12; recommending a separate application is not authorization to build it now.
- A dedicated CLI is negotiable, not an unconditional requirement.
  A new WebUI, editing metadata while extraction is in progress, and libzmx embedding are not required by the answered round.
- NixOS support, automatic audio-CD detection, and preservation of output behavior remain requirements.
  Rsync was preferred for ease and security, not as an end in itself.
  The normal handoff should use the existing NFS mount; do not add a second rsync-over-SSH path without a need.
  A local copy tool, potentially rsync against the mounted path, is an implementation choice rather than another transport.
- The user reports library management running on homeslab and music mounted from the NAS using NFS.
  The inspected configuration keeps the live catalog local and backs it up to the NAS.
  Runtime migration success is user-reported; this planning session has not independently tested the deployed service or network.
  Use zmx instead of tmux/abduco if a selected interactive step needs a persistent terminal.
  A noninteractive systemd service does not need a terminal multiplexer merely to survive client disconnection.

### Q10–Q12, settled review interface

Q10 selects two-way Telegram review with replacement URL/image replies and owned/loaner approval.
Q11 accepts keeping final metadata import separate in beets and asked whether an existing web importer could simplify the workflow.
After the source review below, Q12 A retains Telegram plus the current terminal importer and defers browser integration.
Do not build a WebUI, a second messaging provider, or metadata/release selection inside Telegram.
This settles the planning direction, not permission for bot creation, messages, installation, or unattended library acceptance.

Terminology is recorded in the scoped [CD-ripping glossary](../cd-ripping/CONTEXT.md).
The selected application boundary and rejected alternatives are recorded here; no separate ADR is needed to restate them.
Repository ownership is confirmed.
The direct-copy/recovery contract below still needs implementation fixtures and final plan approval.
Storage/catalog ownership is already established by the external Nix implementation.

## Evidence

### Dotfiles source checkout, not necessarily the live server

- [`fish/functions/rip-cd.fish`](../../../dotfiles/fish/functions/rip-cd.fish) wraps `abcde`, defaults to owned, supports `--loan` and `--keep`, connects through SSH/abduco, rings a terminal bell, and uses rsync before cleanup.
  It retries all `abcde` failures up to 60 failed attempts, not a strict 60-second deadline or a real disc-presence check.
  It transfers/deletes the whole staging root, not just the completed album.
  Remote calls mask SSH failure with `return 0`.
  These failure/cleanup behaviors must not be preserved.
- [`fish/functions/rip-cds.fish`](../../../dotfiles/fish/functions/rip-cds.fish) repeats the single-disc command and handles SIGINT.
  Disc events replace its polling loop, not cancellation/error reporting.
- [`fish/universal-variables/cd-ripping.fish`](../../../dotfiles/fish/universal-variables/cd-ripping.fish) uses `~/abcde`, `Lainatut/` for loans, and `spikenology:/volume2/Media/Music/` as destination.
  These local paths are not the authoritative deployed handoff; see the historical server evidence below.
- [`.abcde.conf`](../../../dotfiles/.abcde.conf) defines the media behavior below and depends on exported `abcde_outdir`.
  A service must supply explicit configuration/dependencies rather than source interactive shell startup.
- [`aliases.zsh`](../../../dotfiles/aliases.zsh) contains older ripping functions with different paths and gain/cleanup behavior.
  Do not migrate those implicitly or treat every shell implementation as equivalent.
- [`scripts/make-links.sh`](../../../dotfiles/scripts/make-links.sh) links `.abcde.conf` only in a Linux-with-display branch.
  That is not a headless-service installation mechanism.
- [`mise/conf.d/shared.toml`](../../../dotfiles/mise/conf.d/shared.toml) declares zmx, but the inspected dotfiles checkout has no CD-specific NixOS module, package, test suite, or mise validation task.

### Current homeslab importer and storage, inspected source

The user reports the cutover complete.
Read-only inspection of `nix-config/hosts/nixos/homeslab/media/music/` confirms the intended integration; it is not an independent deployment, performance, or security test.

- [`beets.nix`](../../../nix-config/hosts/nixos/homeslab/media/music/beets.nix) generates `/etc/beets/config.yaml`, with library `/mnt/media/Music`, catalog `/srv/beets/library.db`, and import state `/srv/beets/state.pickle`.
  The live SQLite catalog is local, not on NFS.
- [`README.md`](../../../nix-config/hosts/nixos/homeslab/media/music/README.md) documents beets 2.14.1 and the existing imports: `beet im --set source=Owned -t /mnt/media/Music/incoming/Owned` and the equivalent `Loaned` command.
  Preserve `source` and use canonical `/mnt/media/Music` import paths so history keys do not alternate between local staging and old NAS-only paths.
  Do not bypass the managed launcher or use global path/config overrides to make an adapter fit.
- [`beet.sh`](../../../nix-config/hosts/nixos/homeslab/media/music/beet.sh) requires UID 1000, takes `/srv/beets/lock` nonblockingly, checks catalog readiness/credentials/storage, then execs beets while retaining the lock.
  A busy importer/backup delays the user's manual import, not extraction or inbox transfer; never bypass the lock to import.
  Extraction and cover review must not hold the catalog lock; a ripper must not take that lock and then invoke the self-locking launcher.
- [`beets-common.sh`](../../../nix-config/hosts/nixos/homeslab/media/music/beets-common.sh) checks ready gates and the exact visible mount target/source, NFS type, writable mode, `hard`, and `sec=sys`.
  Its functions are assembled into the managed scripts, not a standalone public checking command.
  The existing `beet` check does not protect copying to the inbox before the user's manual import; that copier needs the equivalent mount/permission safety contract.
- [`hosts/nixos/homeslab/default.nix`](../../../nix-config/hosts/nixos/homeslab/default.nix) mounts `spikenology.localdomain:/volume2/Media` at `/mnt/media` using a hard NFS automount.
  The `spike` user has UID 1000 and belongs to `cdrom` and `media`.
  Do not substitute service identities or write locally beneath an absent mount without checking the resulting permissions.
- [`beets.yaml`](../../../nix-config/hosts/nixos/homeslab/media/music/beets.yaml) retains `$source/` path templates, move/resume/incremental import behavior and duplicate prompts.
  `fetchart.auto` and `embedart.auto` are false; `embedart.ifempty` is true.
  Merely calling an embed command may preserve the wrong existing image.
  Keep the established library naming, permissions and metadata policies unchanged.
- The nightly backup takes the same lock and uses SQLite `.backup`, copies paired state/config, verifies its NAS archive, and retains completed backups under `/mnt/media/beets/homeslab-backups`.
  Reuse that lifecycle; do not add a second catalog, backup job, or after-rip catalog snapshot.

Earlier in this session, the NAS configuration was read remotely without imports or database writes.
Its `/volume2/Media/Music` and `/volume2/Media/beets/library.db` paths are historical evidence, not the active integration target.

### Historical server evidence and overlapping work

Session `01a16de8-1ecb-717a-bf32-2a4c09fa39a0` reports a read-only server/NAS inspection on 2026-10-24.
The live wrapper then staged `/home/spike/abcde/Owned` and `/home/spike/abcde/Loaned`, uploading with rsync to `spikenology:/volume2/Media/Music/incoming` and excluding both album-art backups and `*abcde.*`.
It reports that beets ran on the NAS and the intended server-reinstallation boundary retained that split.
Those remote wrapper details were not re-read after the migration; the current Nix runbook still documents the old ripper separately from the new homeslab importer.
The dotfiles checkout's `Lainatut/` layout therefore cannot be assumed to describe the actual workflow.
Recheck the deployed wrapper before finalizing transfer paths or claiming migration parity.

Dotfiles change `zklswqxqpnqszvkrqploqrovprxxqkuw`, captured at `ad68418833393663a8ba95bb6b8313be3d479a14`, is a sibling that converts ripping to Nushell/zmx and changes the home-server name to `homeslab`.
It still uses whole-staging cleanup and failed-rip retries.
The user identifies it as an initial idea superseded by wanting logic outside dotfiles.
Leave it untouched and do not require it as a prerequisite; preserve any still-needed hostname configuration when consumer migration is separately authorized.

The earlier ARM evaluation in session `01a173e1-8847-717a-bf32-2a616df102c1` made no ARM implementation or deployment.
It identified the existing single-track MusicBrainz fix, now at `hosts/nixos/homeslab/media/music/abcde-musicbrainz-single-track.patch` and referenced by that directory's `default.nix`.
Related change `lppqwoxnynlsxrnsooktnmmoqklpkvuk` was reported in the server-migration session.
An ARM container uses its own `abcde` and would not automatically inherit that patch.
The user has authorized planning documents in `/Users/oula/gits/nix-config` for the separate beets migration.
This does not authorize modifying its host configuration or installing a ripper there.
The existing session-search and Neovim plans have no apparent code dependency on this work.

### Media behavior from configuration evidence

| Concern         | Baseline to verify against the deployed backend                                                                             |
|-----------------|-----------------------------------------------------------------------------------------------------------------------------|
| Extraction      | `cdparanoia --never-skip=40`, identification through `cd-discid`                                                            |
| Output          | MP3, LAME `--preset insane`                                                                                                 |
| Gain            | `rsgain custom --album --tagmode=i`, from the captured server configuration                                           |
| Cover           | Embedded artwork, always convert, strip image metadata, 4:2:0 sampling, progressive-JPEG options, fit within `1000x1000`    |
| Staging names   | Artist/year/album/track templates, separate single-track templates, padded track numbers, ASCII-safe filename substitutions |
| Tags/completion | `eyeD3 --non-std-genres`, verbose output, cleanup, ejection, terminal bell                                                  |
| Keep            | Upload still runs, but local output remains after successful upload                                                         |

These are staging/media settings, not replacements for the homeslab beets library templates.
The dotfiles checkout's older `.abcde.conf` names `loudgain`, but it is not the deployed gain baseline.
After the user's correction, session `01a16de8-1ecb-717a-bf32-2a4c09fa39a0` was queried for its actual SSH captures.
The old Alpine server on October 24 and restored homeslab on October 25 both had:

```bash
# Use rsgain instead of the default mp3gain for it's far superior
MP3GAIN=rsgain
# This makes rsgain behave like mp3gain and loudgain
MP3GAINOPTS="custom --album --tagmode=i"
```

The old server resolved `rsgain` to `/usr/bin/rsgain` and reported installed package `rsgain-3.7-r1`.
Its `.abcde.conf` symlink pointed to the server's dotfiles checkout; no evidence establishes an executable alias from `loudgain` to `rsgain`.
The current homeslab package list already includes `rsgain`.
Preserve this captured rsgain configuration, not the older local loudgain override.
The emulation comment describes command usage, not proof of identical algorithms, defaults or extra tags.
In particular, the deployed flags do not request lowercase tags or true-peak scanning; do not add `--lowercase`, `--true-peak` or loudgain's `--tagmode=l` to emulate the obsolete local config.
Verify actual gain tags, units, peaks, album handling and Poweramp playback before rollout.

### Direct rsgain integration

The pinned [nixpkgs abcde package](https://github.com/NixOS/nixpkgs/blob/674a2028661e550a97400c1cc992b9cca9a668a1/pkgs/by-name/ab/abcde/package.nix) is 2.9.3.
Its [source archive](https://abcde.einval.com/download/abcde-2.9.3.tar.gz) was downloaded for inspection and matched the package's SHA-256.
`do_replaygain` passes `$MP3GAIN $MP3GAINOPTS` followed by the quoted track-file array; no ReplayGain tool-name whitelist prevents rsgain.
Keep `MP3GAIN` as the executable alone, using the packaged rsgain path, and put `custom --album --tagmode=i` in `MP3GAINOPTS`.
Putting `rsgain custom` in `MP3GAIN` would make abcde's `checkexec` check for an executable named `custom`.
That is a possible explanation for the remembered confusion, not evidence that this was the historical mistake.
A local smoke check executed the pinned `checkexec` and `do_replaygain` functions with stubbed tools and confirmed both the executable check and correct argument boundaries for two filenames containing spaces.
No audio processing, full abcde run, installation or host change was performed.
No new gain patch or executable-name shim is needed for this integration.
Only consider a patch if a reproducible pinned-package failure remains with the correct configuration.

The same [nixpkgs revision packages rsgain 3.8](https://github.com/NixOS/nixpkgs/blob/674a2028661e550a97400c1cc992b9cca9a668a1/pkgs/by-name/rs/rsgain/package.nix).
The inspected [rsgain changelog](https://github.com/complexlogic/rsgain/blob/aae8cc7/CHANGELOG) dates 3.8 to 2026-09-02, confirming the recent update.
Its [custom-mode documentation](https://github.com/complexlogic/rsgain/blob/aae8cc7/docs/rsgain.1) defines `--album` and `--tagmode=i`; [documented defaults](https://github.com/complexlogic/rsgain/blob/aae8cc7/README.md#sample-peak-vs-true-peak) include sample peak rather than loudgain's true peak.
The 3.7-to-3.8 media/parity check remains an implementation/rollout requirement, not a reason to replace rsgain with loudgain.

### Additional abcde source evidence

Inspected [poddmo/abcde `ab0be34`](https://github.com/poddmo/abcde/tree/ab0be34), a development mirror, not proof of the exact installed Nix package.
Its [action expansion](https://github.com/poddmo/abcde/blob/ab0be34/abcde#L4781-L4815) makes `replaygain` imply lookup/read/encode/tag/move, while `embedalbumart` also enables artwork fetching.
Its [artwork step](https://github.com/poddmo/abcde/blob/ab0be34/abcde#L3623-L3825) records `get-album-art=<URL>`, `glyrc`, or `none` in the job's status file.
This supplies a candidate source when retained; `glyrc` is not a URL and must be displayed as an unknown source.
The [final completion code](https://github.com/poddmo/abcde/blob/ab0be34/abcde#L6020-L6083) can report an errors file and still end with `exit 0`.
Therefore process exit alone cannot mark a rip successful, and `clean` must not erase the evidence before discipline validates it.
The implementation must test these contracts against its pinned patched package, not rely on this mirror's line numbers.

### ARM findings

Inspected upstream snapshot [ARM `a380622`](https://github.com/automatic-ripping-machine/automatic-ripping-machine/tree/a380622).

- The [audio helper](https://github.com/automatic-ripping-machine/automatic-ripping-machine/blob/a380622/arm/ripper/utils.py#L444-L477) invokes `abcde` with device/config arguments and log redirection.
  [Bundled config](https://github.com/automatic-ripping-machine/automatic-ripping-machine/blob/a380622/setup/.abcde.conf#L122-L125) is noninteractive.
  That fits unattended ripping and is no longer a reason to reject ARM.
- The [default output settings](https://github.com/automatic-ripping-machine/automatic-ripping-machine/blob/a380622/setup/.abcde.conf#L379-L421) use one output root and artist/album names rather than an ARM job directory.
  Job-isolated staging is not supplied by that layout; repeated releases may target the same location.
  Supplying the current MP3/gain settings through `ABCDE_CONFIG_FILE` is possible, but the service must provide the referenced tools and a valid output root.
- The music path notifies on completion but has no distinct structured audio-completion handoff in the traced code.
  The generic [`BASH_SCRIPT` notification facility](https://github.com/automatic-ripping-machine/automatic-ripping-machine/blob/a380622/arm/ripper/utils.py#L39-L115) receives title/body for entry and other events too.
  `NOTIFY_JOBID` can append the ID to the title, but that still is not a structured completed-album path/event contract.
  Do not make transfer safety depend on parsing notification prose or assume video hooks handle audio.
- [Music-job controls](https://github.com/automatic-ripping-machine/automatic-ripping-machine/blob/a380622/arm/ui/jobs/templates/jobdetail.html#L80-L110) hide the generic title-edit controls.
  ARM's displayed MusicBrainz metadata/art are not proof of changes to output tags; the `abcde` invocation does not receive them.
- [Docker documentation](https://github.com/automatic-ripping-machine/automatic-ripping-machine/blob/a380622/arm_wiki/Docker.md) describes music volumes and network mounts, not a job-isolated review/handoff to this guarded homeslab importer.
  NixOS runtime compatibility, optical-device/event access, ownership, and the image's single-track behavior remain unverified.

ARM supplies detection/status UI but still needs a safe completed-job/import boundary for this workflow.
A standalone service can own per-job staging more directly but also needs application code and NixOS packaging.
Q12 removes the need for a browser review interface, weakening ARM's UI advantage for this workflow.
Q13 A selects discipline, a standalone coordinator around the existing patched `abcde`, with isolated jobs, Telegram review and safe inbox publication.
ARM is a rejected alternative for this change, not a fallback to implement if an experiment fails.
Return for a scope decision if the selected backend cannot meet the bounded compatibility checks.

### Beets artwork and import evidence

- [CLI documentation](https://beets.readthedocs.io/en/stable/reference/cli.html) supports release matching/reimport, field changes, `import --set`, and refreshing externally edited tags.
  Reimport/modify/update may move files, so preserve the existing library naming/ownership contract.
- [Fetchart](https://beets.readthedocs.io/en/stable/plugins/fetchart.html) supports local images and `cover_art_url`; [embedart](https://beets.readthedocs.io/en/stable/plugins/embedart.html) accepts a chosen file or URL.
  These mechanisms are useful building blocks, not an established per-album preview/approve interaction.
- Inspected [beets `5592207`, `embedart.py`](https://github.com/beetbox/beets/blob/5592207/beetsplug/embedart.py#L125-L207) passes `ifempty` into the direct file/URL embedding path.
  That path writes embedded images but does not itself update the album's associated cover file.
  Check embedded art and the album cover together, using the installed version rather than assuming this upstream snapshot matches it.
- [Fetchart's import stages](https://github.com/beetbox/beets/blob/5592207/beetsplug/fetchart.py#L1436-L1504) find and assign art rather than providing the user's existing URL-review prompt.
- The [built-in web plugin](https://beets.readthedocs.io/en/stable/plugins/web.html) browses/plays the existing library and optionally patches/deletes items.
  Its documented API has no import/release-selection operation; it does not supply the requested workflow.
  The homeslab package's explicit plugin allowlist currently excludes `web`.
  Q4/Q7 do not require writing a replacement browser editor.

### Existing web-importer investigation after Q11

Read-only upstream inspection answers the factual question: third-party web importers exist, but the built-in web plugin is not one.
The closest fit among the inspected candidates is [beets-flask](https://github.com/metasauce/beets-flask/tree/00b246d).
Its [README](https://github.com/metasauce/beets-flask/blob/00b246d/README.md) documents inbox previews, alternate candidate selection, GUI imports and a web terminal.
Its [configuration](https://github.com/metasauce/beets-flask/blob/00b246d/docs/configuration.md) supports multiple inboxes and preview-only mode, so automatic library acceptance is not mandatory.
A watched-folder debounce is not proof that a transfer is complete.
Such an integration would need a completion gate; the selected manual workflow instead waits for the transfer-complete notification.

Important differences from the current setup:

- The [candidate selector](https://github.com/metasauce/beets-flask/blob/00b246d/frontend/src/components/import/candidates/candidateSelector.tsx) displays album/track differences and candidate art; the inspected interface chooses releases rather than editing arbitrary metadata fields.
  The [art-preview endpoint](https://github.com/metasauce/beets-flask/blob/00b246d/backend/beets_flask/server/routes/art_preview.py) retrieves images for display, not URL/file replacement of the staged or imported cover.
  No complete cover-replacement workflow was found in the inspected UI/routes.
- The [import enqueue contract](https://github.com/metasauce/beets-flask/blob/00b246d/backend/beets_flask/invoker/enqueue.py#L260-L269) accepts candidate IDs and duplicate actions, not a per-album ownership field.
  Its importer applies configured `set_fields`, but a global value or separate inboxes are not an existing owned/loaner selection control.
  Supplying this interaction would still require adaptation.
- The inspected [package metadata](https://github.com/metasauce/beets-flask/blob/00b246d/backend/pyproject.toml) identifies version 1.2.1 as beta and pins `beets==2.5.1`, versus the homeslab runbook's 2.14.1.
  Compatibility with the current catalog/configuration is untested; do not downgrade the managed installation or open its catalog with the bundled version as an experiment.
- Its [documented limitations](https://github.com/metasauce/beets-flask/blob/00b246d/docs/limitations.md) support only copy imports, unlike current `move: true` semantics.
  Retention/cleanup would need an explicit reconciliation; a successful copy is not permission for shared-root deletion.
- beets-flask's [import session](https://github.com/metasauce/beets-flask/blob/00b246d/backend/beets_flask/importer/session.py#L210-L229) opens the library through beets' Python API, not the guarded executable.
  A persistent UI cannot simply run through the present guarded launcher: that launcher retains the backup/import lock until its child exits.
  Running the UI against the catalog independently would instead bypass the existing checks/lock.
  Any adoption must establish guarded operation-level access rather than block nightly backups indefinitely or open a second unguarded writer.
- The [web terminal](https://github.com/metasauce/beets-flask/blob/00b246d/backend/beets_flask/server/websocket/terminal.py) uses tmux, whereas the GUI importer uses the Python pipeline directly.
  Do not use that terminal as the workaround for missing review controls under the user's zmx requirement; disabling or replacing it would need a packaging check.
- A browser UI also needs an authorized access/authentication plan and usable phone interaction.
  None was deployed or tested, and replacing an importer is not automatically less maintenance than the bounded Telegram review.

GitHub metadata checked during this round marks beets-flask unarchived, with activity on 2026-10-17 and latest published release [v1.2.0](https://github.com/metasauce/beets-flask/releases/tag/v1.2.0) dated 2025-12-17.
That establishes recent project activity, not production readiness or compatibility with this host.
[Beetiful](https://github.com/Vansmak/beetiful/tree/1d75be7) is archived according to its repository metadata.
Its [command builder/config editor](https://github.com/Vansmak/beetiful/blob/1d75be7/README.md) is not a candidate/cover-review importer, and the README lists mobile friendliness as future work.
Do not expose command execution or editing of Nix-generated configuration to simulate the missing review UI.
The inspected [Beetle discussion](https://github.com/beetbox/beets/discussions/6209) describes a browser/player using the standard beets web API, not a replacement import pipeline.
Do not mistake those library browsers for evidence that all required review steps already exist.

Q12 A retains Telegram review and the existing importer.
These findings record why browser import integration is deferred; they are not outstanding work for the ripper change.
Revisit only if browser import becomes a separate requirement and its ownership/artwork/guarded-access gaps can be bounded.

### Phone interaction feasibility

- Telegram's [sendPhoto](https://core.telegram.org/bots/api#sendphoto) accepts an uploaded image and caption; [inline keyboards](https://core.telegram.org/bots/api#inlinekeyboardmarkup) support ownership/approval buttons.
  [getUpdates](https://core.telegram.org/bots/api#getupdates) receives replies and callbacks through long polling, so this integration need not expose a public inbound endpoint on homeslab.
  [getFile](https://core.telegram.org/bots/api#getfile) supports retrieving a submitted image/file; file links containing bot credentials must not appear in logs or user-visible source URLs.
- Discord supports buttons/interactions and replies through an application.
  Its [interaction overview](https://docs.discord.com/developers/interactions/overview) permits either Gateway delivery or a public HTTP interactions endpoint; the [Gateway](https://docs.discord.com/developers/events/gateway) is an outbound WebSocket connection with reconnect/session handling.
  Discord therefore need not expose a public homeslab endpoint either, but a posting webhook alone is not a two-way review bot.
- Q10 and Q12 select one private Telegram bot with long polling.
  Do not build both providers or a transport-plugin framework.
  These API capabilities do not prove our backend can retain the original cover URL or safely apply a reply; those require bounded implementation checks.
- ARM's inspected generic notification facility is not evidence of authenticated, job-specific replies that change cover art and ownership.
  Count that adaptation when comparing ARM with a standalone coordinator.

## Established library-management boundary

The [current Nix music module and runbook](../../../nix-config/hosts/nixos/homeslab/media/music/README.md) now own mount configuration, catalog/state, locking, backup/restore and migration history.
The earlier separate planning documents are no longer present in that checkout; link to the maintained implementation rather than keep a stale migration prerequisite.
These cross-repository links assume sibling `discipline`, `dotfiles` and `nix-config` checkouts and do not authorize edits to their source/configuration.

Target only the existing homeslab importer and mounted music tree.
Do not build a NAS-execution fallback, reopen the old NAS catalog, switch to SMB, or implement transport abstraction.
The ripper consumes the established storage contract but still owns safe publication of its own completed output.
If a required integration cannot fit without changing the external module, stop and request a scope/owning-repository decision rather than silently adding a prerequisite change.

## Intended responsibilities

- Ripping component: detect audio media, own the drive, extract/encode into isolated per-job staging, report success/failure, and eject when extraction is safely complete.
  It need not know owned versus loaner to read audio.
- Review/handoff component: collect classification, present the candidate cover for approval or a replacement URL, and hand off only complete jobs to the selected beets workflow.
  Preserve explicit job identity across disconnects/retries; do not use album title or MusicBrainz release ID as an ownership key.
- Existing guarded homeslab beets workflow: final release matching, tags, library paths and duplicate decisions, preserving `source=Owned` or `source=Loaned`.
  Loaner-to-owned changes and future library organization remain outside this project.
- Inbox transfer: copy a complete, ownership/cover-reviewed local rip directly into its classified disc-specific NFS inbox directory, allowing overwrites there.
  Retain local output on failure and remove only that local attempt after verified transfer unless keeping it.
  Never overwrite the managed library, another disc/category, or delete a shared staging/inbox root.

Selected flow: local extraction under `/srv/abcde` → Telegram notification → ownership/cover approval → direct copy into the disc-specific directory under `/mnt/media/Music/incoming/Owned` or `Loaned` → existing interactive beets import.
The mounted inbox is not final library acceptance; metadata/release matching still happens in beets.
Q11 explicitly accepts this separation rather than requiring metadata approval before the inbox write.
Final import remains the existing manual terminal step; the new application stops at the prepared inbox.
Do not start a detached interactive importer that holds the catalog lock while waiting indefinitely for someone to attach.

Keep optical extraction on local disk, independent of NFS availability and the beets/backup lock.
An NFS outage may block a publisher on the hard mount; no timeout permits deleting its source or reporting success while I/O is unresolved.
The local disk and NFS mount are different filesystems, so the requested move is copy, verify, then remove the local attempt, not an atomic cross-filesystem rename.
The inbox may contain partial or mixed old/new files during transfer; do not import that disc until its transfer-complete notification.
For a category-wide import, wait for all approved transfers into that category to finish and do not approve/retry more until the import ends.
Keep local attempts separate; an explicitly approved newer rip may overwrite the same disc's inbox directory.
A beets duplicate skip/cancellation is not automatically a failed rip or authorization to remove remaining inbox files.
The final implementation must distinguish publication, library import and local cleanup instead of relying solely on an importer exit code.
A selected URL must produce the approved embedded and sidecar artwork without a later automatic fetch replacing it.
Do not make the live beets database reference staging that transfer cleanup will delete.

### Future beets import WebUI

Recommendation in response to Q18: a separate application/project, not a browser frontend embedded in discipline.
Discipline owns physical-disc extraction, rip review and delivery to the classified inbox.
An import WebUI would own release matching, metadata edits, duplicate decisions and beets import sessions, including music that was not ripped by discipline.
The existing inbox is the seam between these responsibilities; choosing Go for the ripper does not choose the WebUI's language or require it to share a runtime.
Both applications could run on homeslab and be deployed through the same host configuration without sharing process or catalog ownership.

Keep the existing manual importer for this change.
Do not add a web server, shared SDK/database, speculative HTTP interface, second review provider or completion protocol merely to prepare for the future UI.
The future project's plan must decide its own authentication and guarded beets integration, preserving catalog readiness/backup locking rather than wrapping a persistent server in the lifetime-lock launcher or bypassing it through the beets API.
It must also coordinate with in-progress inbox copies; directory appearance alone is not a completion signal.
Those decisions become prerequisites when the WebUI is scoped, not additional work or unanswered product decisions for this ripper change.
The separate-project recommendation remains open to that future discussion, not a newly approved second application.

## Telegram review contract

Q12 confirms two-way Telegram review and defers browser import integration.
Use this contract for the selected application:

- Notify after successful local extraction/processing, not on insertion or library import.
  Say "ripped, awaiting review", include a stable job identifier, tentative album information, the actual candidate image and its public source URL when known.
  For a submitted local image with no public source, say so; never invent a URL or expose a private filesystem/download-token URL.
- Correlate button actions and direct replies to that exact job/message, not the most recently ripped disc or album title.
  Multiple pending CDs must not mix ownership or images.
- Let the authorized user reply with a replacement URL or an image attachment.
  Validate/download/convert it before replacing the staged candidate, update the preview, and require explicit approval of the resulting cover.
  Preserve the previous candidate if processing fails.
  A platform-resized photo is not guaranteed to be the original; support an image sent as a file when original quality matters.
- Proposed approval buttons are "Approve owned" and "Approve loaner", accepting the displayed cover and choosing the existing `source` category together.
  No response is not approval and never supplies a default ownership category.
  Late replies after publication must not silently rewrite the managed library.
- Allow only the configured chat/channel and user identities to mutate a job.
  Keep secrets outside the repository/Nix store, bound image size/dimensions and fetch time, reject unsafe URL destinations/redirects, and prevent shell/path injection.
- Persist pending jobs, message associations and applied-action state locally.
  Duplicate/replayed updates must not enqueue another transfer or overwrite a newer review.
  Explicit retry of an interrupted copy is allowed; completed or superseded attempts never automatically resend.
  Messaging delivery is not durable job storage; an API outage must leave extraction output and pending reviews intact.
  A notification may need retry after an ambiguous send failure; do not promise exactly-once message delivery.
- Send only the needed album text, image and source link to the provider, not audio files, catalog contents or credentials.
  Provider selection includes accepting that limited third-party disclosure.
- Q11 accepts phone approval stopping at a prepared inbox, with final metadata/duplicate review in the existing importer.
  Its web alternative does not authorize rebuilding those interactions inside Telegram.

These rules are not authorization to create a bot or deliver a test notification.
If safe two-way review and the ripper cannot fit one reviewable change, ask to narrow the first delivery or choose `/plan-stack`; do not silently add another change.

## Ripper change boundary

Implement discipline as one application/package change in its dedicated repository, using mature media backends and including its NixOS module.
Do not add an ARM integration, custom metadata editor or host migration to that change.
The proposed files, dependencies, command/interaction contracts and rollback below define the one-change scope.
Q17 settles stale-file removal within the selected disc's inbox directory.
Dotfiles consumer migration and live deployment must stay separate if they require other repositories.
Beets relocation, mount configuration, catalog backups, library-wide reorganization and NAS-shell compatibility are excluded.
Preserve the existing patched `abcde` unless a measured compatibility gap justifies a different backend.
Q13 selects one standalone application change in a dedicated repository, including its Nix package/module and focused checks.
Keep extraction, encoding and gain in mature media tools; the application owns job state, event handling, review and publication rather than copying the shell wrapper into another repository.
A dedicated repository avoids making dotfiles or the host configuration own the application logic.
Host enablement and retirement of old shell consumers remain separately authorized rollout steps, not hidden commits in this one-change plan.
Q14 confirms `/Users/oula/gits/discipline`; read-only inspection found no existing files, tooling or repository there to reuse.
The user's later request authorizes repository initialization and document relocation, now completed without starting implementation.

For discipline:

1. Audio insertion starts one extraction job without human input.
   Deduplicate repeated events; handle a loaded disc at startup and successive discs.
   Do not run ripping or network work inside udev; [systemd's udev documentation](https://www.freedesktop.org/software/systemd/man/latest/udev.html) forbids that long-running work there.
   Use the insertion and interruption policy specified below; do not guess that a MusicBrainz ID identifies a physical insertion.
2. A job is not reviewable/importable as complete until all required audio processing succeeds.
   Repeated albums and concurrent events must not overwrite another local attempt; approved rerips may replace that disc's inbox files.
3. Review must support the user's cover habit: inspect the candidate and accept it or supply another URL.
   Final metadata remains compatible with the established beets workflow.
4. A client disconnect does not cancel ripping or lose saved choices.
   Use zmx only for interactive steps that need a persistent PTY; do not select Zig merely to embed libzmx.
5. Failures keep actionable status and recoverable output.
   Never repeat extraction merely to retry publication; preserve the durable job/attempt receipts described below.

## Proposed implementation contract

The behavior and application boundary below remain the proposed contract.
Q18 supplies the Go preference; the language-specific proposal below follows it, with final plan approval still outstanding.
No implementation files have been created.

### Files and tooling in the discipline repository

Use Go for the coordinator, with standard-library `os/exec`, `net/http`, `encoding/json`, `flag` and `database/sql`.
Use `github.com/mattn/go-sqlite3` as the SQLite driver, `github.com/jochenvg/go-udev` for Linux device events, and `golang.org/x/sys/unix` for descriptor-relative filesystem operations and locks.
Pin dependencies in `go.mod`/`go.sum`; no Telegram SDK or CLI framework is needed.
The inspected [SQLite driver](https://github.com/mattn/go-sqlite3/blob/dbbe931/README.md#installation) and [libudev binding](https://github.com/jochenvg/go-udev/blob/b65ed64/monitor.go) both require CGO; this is a Linux/Nix package, not a promised dependency-free static binary.
The monitor enables receiving before returning its event/error channels, which supports the subscribe-before-enumerate rule below.
Drain both channels while running and shutting down; an unexpected closure/error requires reconciliation, not an inferred disc insertion.
Source inspection establishes the intended interfaces, not a successful build or hardware check.

Use existing media executables rather than implementing extraction, encoding or image conversion.
Keep the coordinator in one internal Go package split by responsibility; do not create a reusable framework or public SDK for the hypothetical WebUI.
No web server, message broker, ORM, plugin framework or beets library dependency is needed.

| Planned path                                                                    | Responsibility                                                                                 |
|---------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------|
| `go.mod`, `go.sum`, `cmd/discipline/main.go`    | Package metadata and the `discipline` command                                                  |
| `internal/discipline/jobs.go`                                                        | SQLite state transitions, durable command requests, approvals, manifests and receipts          |
| `internal/discipline/drive_linux.go`                                                       | One configured drive, libudev monitor and insertion reconciliation                              |
| `internal/discipline/media.go`, `data/abcde.conf`, `data/imagemagick-policy.xml`     | Per-job abcde invocation, completion validation and bounded cover processing                   |
| `internal/discipline/telegram.go`                                                    | Long polling, previews, authorized replies/buttons and repeat-safe updates                     |
| `internal/discipline/transfer.go`                                                    | Direct, mount-safe rsync copying, overwrite/retry policy and verification before local cleanup |
| `nix/package.nix`, `nix/module.nix`, `nix/abcde-musicbrainz-single-track.patch` | Wrapped dependencies, two service units and the preserved single-track fix                     |
| `flake.nix`, `flake.lock`                                                       | Linux package, NixOS module, dev shell and checks                                              |
| `internal/discipline/*_test.go`, `testdata/`, `nix/tests/service.nix`, `README.md`                                  | Focused contract tests, isolated service test and operation/recovery instructions              |

Start from the host's root nixpkgs input revision `674a2028661e550a97400c1cc992b9cca9a668a1`, read from `nix-config/flake.lock`, not an unrelated transitive input.
Use that nixpkgs revision's Go toolchain and `buildGoModule`, with a pinned `vendorHash`, `CGO_ENABLED=1` and the libudev development/runtime dependency supplied by Nix.
The SQLite driver supplies its bundled SQLite source; no separate database server is needed.
Declare dependencies in `go.mod`/`go.sum` and Nix; do not rely on the interactive host PATH or dotfiles.
Wrap patched `abcde`, `cd-discid`, `cdparanoia`, LAME, `rsgain`, eyeD3, ImageMagick, artwork-fetch dependencies used by that abcde version, curl, rsync and util-linux explicitly.
Generate the direct rsgain configuration specified above; do not depend on a host-local loudgain alias or symlink.
Copy the existing single-track patch into this repository with provenance; do not add a runtime import of the sibling `nix-config` tree.
If the pinned source already contains that fix, prove the single-track case instead of applying a duplicate patch.
Use Go's `testing` and `net/http/httptest` with temporary state/media fixtures; no Python coordinator or pytest.
Existing media tools retain their own runtime dependencies.
Run formatting checks and `go test -race ./...` through the Nix checks; keep `nix flake check` and `nix build .#discipline` as the package validation entry points.
The Linux build/service fixture must prove CGO/libudev linkage, monitor startup/shutdown and durable SQLite behavior across both processes before merge.
If a binding fails that bounded check, fix or replace the binding without changing the chosen language or the workflow; do not hide the failure behind a polling loop.
Linux service/package checks require a Linux builder, not an unverified macOS substitute.

### Process and state ownership

Run `discipline.service` for drive handling, extraction and Telegram; run `discipline-publisher.service` separately for NFS work.
Both use the same local `/var/lib/discipline` state, but hold no SQLite transaction across subprocess, network or NFS I/O.
The separation is required so blocked hard-NFS I/O cannot stop extraction or review.
One publisher process holds a local advisory lock for its whole lifetime; its systemd unit must not kill/restart it into a competing unresolved attempt.
Use `TimeoutStopSec=infinity` for that unit and document the operational cost of a blocked mount.
The main service does not require the NFS mount to start.
Within it, drive events, extraction subprocess waits and Telegram/image requests run independently; stalled long polling or notification retries cannot delay a new extraction, and extraction cannot block review.
Use bounded network calls and a serialized local state transition handler rather than running blocking work inside event callbacks.

The module requires an existing service user, one drive path, expected media mount/source, one Telegram chat/user allowlist and a bot-token credential file.
For homeslab the intended user is `spike`, UID 1000, with `cdrom`/`media` access; host enablement supplies those existing values later.
Use systemd credentials for the token, private local state and an explicit device permission policy; do not use a root daemon or put the token in the Nix store.
Provide `keepLocal` to preserve the established keep behavior, default false after verified publication.
No configuration for multiple providers, drives, transports or import backends in this first change.

Keep `state.sqlite3` and logs under local `/var/lib/discipline`; use `/srv/abcde/<job-UUID>/` for each attempt's scratch and media.
Do not reuse or clear another attempt's local directory when reripping a disc.
Persist insertion identity, phase, last error, art revision/digest/source, ownership, Telegram message/update identity, media manifest and transfer outcome.
Track the newest-created approved attempt for each category/disc destination so an older retry or late approval cannot overwrite a newer approved rip.
Use durable SQLite commits, and flush media before treating a phase as complete.
Retain small terminal job receipts indefinitely when audio is cleaned.
Missing/corrupt existing state, or orphan job directories without receipts, stops recovery rather than silently starting a new database or reconstructing publishable jobs.
Restoring an older discipline database requires manual reconciliation; the separate beets backup does not cover this state.

### Insertion and media lifecycle

Subscribe before startup enumeration and serialize both through the same presence transition.
An absent-to-audio-present transition creates one UUID; repeated events for that insertion never create another job, including after a failed rip.
An observed removal and reinsertion creates a new job even for the same disc.
On restart, matching loaded media stays associated with the prior insertion; removal/reinsertion of the same CD while the monitor was stopped is unknowable and requires explicit `new-insertion`.
Data-only media is ignored, and mixed-mode media is held with an unsupported-media status in the first version rather than risking a data track.
A failed/interrupted rip retains output and requires explicit retry with the intended disc verified; there is no failed-abcde polling loop.
Do not eject after an extraction failure; after verified processing success, eject and allow the next disc while earlier jobs await review/publication.
Low disk space stops the affected job with an actionable error; do not evict pending jobs to make room.

Give abcde a private HOME, working directory, scratch/output roots and generated config with trusted values.
Run noninteractively with `-N` and the equivalent of `replaygain,getalbumart`, omitting `clean` and delaying cover embedding until approval.
Keep the existing MP3, gain and filename settings; do not re-encode audio to change artwork.
Require all expected audio tracks, successful read/encode/tag/move/gain evidence, no recorded audio-processing errors and valid output files before declaring the rip complete.
A zero exit status without that evidence is failure.
Treat absent metadata/artwork as a review problem rather than silently choosing an owned category or inventing a cover source; the backend fixture must prove unknown-disc extraction is noninteractive.
Preserve scratch until its status and metadata have been recorded; never source its status/log contents as shell code.
Apply the same image decoder policy and bounded download handling to abcde's automatic candidate retrieval, not only Telegram replacements.
Validate that configuration against the pinned backend; do not let a fallback provider bypass the limits.

### Review and finalization

A completed rip with a candidate cover gets the Telegram preview; missing art gets a text notification requesting a URL or image.
No publication without an explicitly approved image and category; an unwanted job can be abandoned through the local recovery command.
A replacement increments the review revision and invalidates older approval buttons.
Approval freezes the category and exact image revision; reject further edits once finalization/publication begins rather than racing a write.
Store an applied update and its effect together before advancing the Telegram polling offset.
Duplicate message delivery is tolerable; duplicate effects are not.

Accept JPEG, PNG or WebP input up to 10 MiB and 25 megapixels, with a bounded decoder resource policy and a 30-second processing deadline.
Convert the approved image with the existing strip/sampling/progressive-JPEG/1000x1000 settings, replace front-cover tags deliberately, and verify both embedded and sidecar art.
Use argument arrays, not interpolated shell commands; reject path escapes, symlinks and unexpected output types.
For user URLs, allow HTTPS only, reject userinfo/private/loopback/link-local destinations, validate each of at most three redirects, and pin the validated address for the actual request rather than re-resolving it unchecked.
Use curl's address-pinning capability with environment proxies disabled; Telegram API/file URLs are constructed separately and credentials are redacted.
Downloads have the same byte cap and a 30-second deadline; failed replacement preserves the last candidate.

### Superseded no-clobber probe

The first connection carrying Q15's probe failed SSH-agent authentication before execution.
After the user restored signing and requested a retry, the authorized probe ran at `2026-10-27T07:59:34Z` as `spike`, UID 1000, on kernel `6.18.54`.
Before writing, it checked the readiness marker, expected source/target, NFS type, `rw,hard,sec=sys`, no-follow directory traversal and equality of the pinned directory's mount ID with the visible mount.
The observed filesystem was NFS 4.1 from `spikenology.localdomain:/volume2/Media` at `/mnt/media`.

| Probe operation | Observed result |
| --- | --- |
| `renameat2(RENAME_NOREPLACE)` to absent destination | `EINVAL`, source unchanged, destination still absent |
| Same operation with existing empty directory | `EEXIST`, source and destination identities unchanged |
| Same operation with existing populated directory | `EEXIST`, source and destination identities/content unchanged |
| File, directory and containing-directory `fsync` calls | All returned success |
| Identity-checked cleanup | All created objects removed; scratch directory absence verified |

Scratch was `/mnt/media/Music/.discipline-plan-probe-83dc7c4c62954f58941021a48d7a6903`, not an edit target or permanent application directory.
The absent-destination failure makes the original no-replace publication method unusable on this mount.
It does not prevent direct copying with overwrites, which the user has now selected.
The result remains historical evidence, not a blocker or reason to change transport.
Successful flush calls establish basic call support, not server power-loss durability or NFS replay behavior.
No music, beets catalog/state, configuration or service was modified.

### Direct inbox transfer and rerips

Use rsync against the existing mounted path, not a second NAS staging directory or another SSH transport.
The inbox is disposable; an approved rerip may overwrite files there.
Keep the managed library, other disc directories and local attempts outside that overwrite scope.

Separate the identity of a disc from the identity of an attempt.
Use a deterministic SHA-256 fingerprint of the normalized audio TOC as `disc-key`, computed independently of metadata lookup.
Include track count, ordered track offsets and lead-out; store the normalized input with the job.
This gives unknown discs a stable key too; a MusicBrainz release ID alone would conflate discs in a multi-disc release.
A TOC fingerprint identifies a disc layout, not a physical insertion or a cryptographic identity of its audio.
Each extraction attempt still has its own UUID and local directory.
Preserve abcde's existing relative album/track naming inside that directory.

1. Finish local extraction, cover approval/embedding and ownership selection; freeze the media manifest.
2. Claim the job and record `copying` before writing into `/mnt/media/Music/incoming/<Owned|Loaned>/<disc-key>/`.
   Revalidate the nonempty source tree against the frozen manifest before allowing destination changes.
   Copy only that complete media tree, never its scratch/logs or all of `/srv/abcde`.
   Q17 accepts overwriting regular files and removing stale contents within this exact destination; use rsync's `--delete-delay`, without `--ignore-errors` or broader deletion scope.
3. Check rsync success, the exact destination media file set, file hashes and required flush results before recording `published` locally.
   Destination existence or rsync's size/mtime shortcut alone does not establish that the approved bytes arrived.
   Keep the local attempt if copying or verification fails.
4. Notify that transfer is complete and the disc is ready for manual import.
   Only then remove this attempt's local data unless `keepLocal` is set; retain its small receipt.

Validate the readiness marker, expected mount/source/type/options and write access before any destination write.
Pin the validated destination root so a missing/replaced mount cannot redirect copying into the uncovered local mountpoint.
Reject path traversal, symlinks and unexpected file types; permitting overwrites does not permit following a link outside the selected disc directory.
The isolated publisher serializes transfers and checks the newest-created approved attempt for the category/disc before starting.
A late approval/retry for an older attempt must not overwrite a newer approved rip.
Do not remove another disc's directory, a category root, the managed library or the local staging root.
There are no remote reservation markers or automatic remote housekeeping jobs.

### Importer coordination and interrupted transfers

This design deliberately does not provide atomic whole-album visibility.
Do not run an import or another writer against the affected disc directory during copying or retry.
For a whole-category import, wait for all approved transfers to finish and do not approve/retry more into that category until import ends.
An interrupted transfer may leave incomplete or mixed old/new inbox contents; Telegram/status must say it is not ready, while the local rip remains available.

After quiescing a failed copy, an explicit retry may overwrite the same destination without ripping again.
After a crash or uncertain outcome, first check whether beets has already consumed files or a newer attempt has replaced them.
Never automatically resend a completed/superseded attempt because its inbox path is missing.
Keep the local receipt/reconciliation commands for this distinction, without reservation identities or a rename fence.
`published` means transfer verified, not accepted into the managed library.

Stable rerip paths also interact with beets' configured incremental/resume history.
For an intentional rerip, document a manual guarded import of that exact disc directory with `--noincremental --noresume`, retaining `--set source=Owned` or `Loaned` and `-t`.
Those per-invocation options preserve interactive release/duplicate decisions and do not change global beets configuration.
The inspected [beets import CLI](https://github.com/beetbox/beets/blob/5592207/beets/ui/commands/import_/__init__.py) defines both options; prove the rerip behavior in a disposable fixture against the installed version before rollout.
No live catalog/import is part of this planning work.

### Minimal recovery commands

`discipline jobs` and `discipline show <UUID>` report local state, errors, paths, approval/receipt and permitted next actions without touching NFS.
`discipline retry <UUID>` queues only the failed safe phase; it never resets approval or rerips to retry a copy.
`discipline reconcile <UUID> --outcome published|not-published|abandoned --note <reason>` requires explicit confirmation and an audit record.
Before reconciling an uncertain copy, quiesce its publisher and the manual importer; `not-published` means the operator has confirmed that retry will not resend already imported audio or overwrite a newer rip.
An absent inbox or beets incremental history alone does not establish that.
Every worker/retry/abandon request must atomically claim the expected job phase; only one may own a job's files at a time.
Abandonment becomes cleanup-eligible only after extraction, finalization and copying workers have quiesced; a queued request never permits deleting files still in use.
Reject abandonment as a way to reset a published receipt, and never reconcile an unresolved worker by merely changing its database status.
`discipline cleanup <UUID>` claims a published/abandoned idle job, then removes only its registered local data while retaining its receipt; automatic cleanup honors `keepLocal`.
`discipline new-insertion` explicitly overrides the offline same-disc ambiguity after verifying the configured drive has no active extraction.
Mutating CLI commands enqueue durable requests for the running application, not ad-hoc state edits or a second writer performing filesystem work.
Report queued versus completed actions distinctly; no retry-all, force-republish, catalog mutation or remote recursive cleanup command.

## Verify

No new CD-specific task exists here; bind checks to the selected package's declared tooling before marking the plan ready.
The Q15 storage probe and stubbed gain-dispatch check are recorded above.
The following remain implementation/deployment acceptance targets, not completed end-to-end checks.

| Bounded check | Observable success | Failure action |
| --- | --- | --- |
| Go package and event handling | Linux checks compile/link CGO dependencies, race-check concurrent workers and prove SQLite durability/claims; libudev startup/shutdown drains event/error channels, and fixtures create one job per observed insertion, deduplicate startup/events/restarts, ignore data-only media, hold mixed-mode media and require explicit override for offline same-disc reinsertion | Fix the binding or lifecycle before merge; do not hide failures behind failed-rip polling |
| Drive/runtime | Separately authorized hardware check confirms device permissions, extraction/ejection, single-track identification and repeat insertion on homeslab | Block deployment; preserve the working backend/patch |
| Job isolation | Two rips of the same release have separate staging, and incomplete output cannot enter review/import as complete | Block handoff; do not infer completion from directory appearance or notification text |
| Backend completion | Pinned-package fixtures cover zero exit with recorded errors, missing tracks, failed gain, unknown metadata and missing art; only complete audio processing reaches review, without prompts | Block the backend; do not equate exit zero with success or publish partial tracks |
| Media parity | Temporary output preserves encoding, tags, approved art and naming; the pinned package invokes real rsgain with the captured options and exact track list, matching the deployed baseline for album/track gain, peaks, tag case/units and playback; gain tagging and cover replacement preserve audio frames | Block rollout on a regression; do not substitute loudgain or add obsolete flags based on the older local config |
| Cover replacement | Start with a wrong existing embedded image and cover file; approve another URL or uploaded image; inspect both final images and their conversion properties | Fix the bounded art step or return for a tool decision; `ifempty` and disabled auto plugins must not yield false success |
| Phone preview | A completed fixture job produces the correct candidate preview/source link and pending-review status; failed notification does not lose the job | Fix the selected provider integration or return for a scope decision; do not add a second provider as fallback |
| Phone actions | Two pending jobs, wrong-user/chat actions, duplicate/stale replies, restart and invalid/oversized images cannot affect the wrong job or publish twice; approved valid replacement reaches sidecar and embedded art | Block two-way mutation until authenticated, job-specific and repeat-safe |
| Image boundary | Local HTTP fixtures test private/loopback/IPv6 destinations, redirects, DNS rebinding/address pinning, limits, malformed images and token redaction without sending real bot messages | Reject the input and retain the previous candidate; no arbitrary file/URL reads |
| Ownership/import | A disposable library with a sanitized copy of the actual import/path settings routes owned and loaner fixtures correctly; unclassified jobs cannot reach final import | Stop for an import contract decision; never experiment on the live catalog |
| Disconnect | Close/reopen the selected interface during extraction/review without duplicate work or lost decisions; test zmx only where selected | Resolve process/state ownership before rollout |
| Disc identity and rerips | Unknown discs and changed metadata retain the same TOC key; different disc layouts have different keys; attempts have isolated local paths; an approved newer attempt overwrites its own inbox files and older retries cannot replace it | Fix identity/ordering; never use album titles or release IDs alone as the destination key |
| Direct NFS copy | Absent/wrong/read-only mounts and destination symlinks receive no fallback/escaped writes; failed copies retain local media and report the partial inbox as not ready; success verifies approved bytes before cleanup | Stop transfer/cleanup; do not reintroduce NAS staging or claim atomic album visibility |
| Rerip contents | Changed track/album names replace the old contents without stale files; an empty/missing/corrupt source fails before destination changes; deletion stays within one category/disc directory, leaving another disc and the managed library untouched | Block transfer/cleanup until source validation, exact mirroring and deletion scope are correct |
| Crash recovery | Interrupt during copy, after verification before receipt commit, and during local cleanup; explicit quiesced retry can repair incomplete copying, while completed/superseded attempts never automatically resend after beets consumption | Keep local output and reconcile uncertain outcomes; never delete source on a partial copy |
| Worker exclusion | Concurrent retry, abandon and cleanup requests cannot claim the same job or delete files held by an extraction/finalization/publisher worker | Reject the conflicting action until the worker quiesces; do not override state manually |
| Process isolation | Stall NFS while extraction, review and local status progress; separately stall Telegram/image requests while a new disc starts extraction; restarting the main service never creates another publisher attempt | Fix scheduling, process ownership and lock lifetime; do not turn a timeout into a competing retry |
| State loss | Corrupt/missing state with existing spool refuses to reconstruct jobs or publish, and documented restore requires reconciliation | Fail closed; never infer unpublished work from orphan directories |
| Manual importer handoff | Commands use the guarded launcher, correct identity/category and only completed transfers; a disposable library imports a rerip at the same path with incremental/resume disabled for that invocation and still prompts for duplicates; the application never takes the catalog lock or starts an import | Fix the handoff documentation/fixture; never bypass the launcher, rely on incremental history to detect replacement, or open the live catalog |
| Cleanup | Keep mode preserves local output; successful verified publication removes only permitted local staging; duplicate skip/cancellation does not delete unconsumed inbox files | Block cleanup until stage-specific outcomes are established |

Record the installed backend/beets versions and exact preserved single-track fix before comparison.
Use disposable directories/databases for any later test and ask before hardware tests, host activation, or shared-library writes.
Remote work comprised selected configuration reads and the authorized Q15 probe, with additional local source/API-documentation inspection.
The successful retry wrote only its hidden scratch objects and verified their removal.
No further probe is authorized or needed for the superseded reserved-directory design.
The direct-copy/rerip workflow still needs its implementation fixtures; none have run.
The local pinned-abcde gain-dispatch smoke check passed with stubbed tools; actual rsgain audio/tagging and playback checks remain unrun.
No ripping, imports, mounts, runtime benchmarks, backup jobs, bot creation, test messages or service changes ran.
No speedup or production NixOS compatibility is claimed.

## Dependencies and status

Q9 is resolved and the user reports the external beets/NFS work complete.
Q10–Q12 settle two-way Telegram review and a separate terminal beets import; browser integration is deferred.
Q13 selects discipline around patched `abcde`, and Q14 confirms `/Users/oula/gits/discipline`.
The user authorized repository creation and document relocation; the jj repository now exists with these planning documents, but no implementation files.
The proposed layout, command contract and recovery behavior are now specified above.
The Q15 capability check is complete and rejects the original publication operation; SSH-agent access is no longer the blocker.
Q16 is superseded by the user's disposable-inbox/direct-copy clarification.
Q17 accepts the recommended per-disc replacement policy, including stale-file removal.
Q18 prefers Go; the file layout, dependencies, Nix build and tests now reflect that choice.
The future import WebUI's detailed design is deferred to its own scope; it does not require Python in this application or a second change here.
Final plan approval and a separate implementation request remain outstanding; repository creation and document relocation are complete.
No additional no-clobber capability probe is a gate; the listed application fixtures are implementation/merge checks, not a request for more planning-time shared-storage writes.
Do not call `/implement` in dotfiles or treat repository initialization as implementation permission.
The guarded import commands and `source` values are now known from the current runbook; they are no longer open inventory questions.
Preserve the existing catalog ready gate, mount contract and backup lock rather than implementing their prerequisites again.
Historical remote captures resolve the gain-tool/configuration discrepancy in favor of rsgain.
Fresh deployed-state checks and real rsgain 3.8 output/playback verification remain rollout gates, not permission to revert to loudgain.
The current Nix package references the single-track patch, but this planning session claims no new hardware test.
The existing Nushell conversion is not a prerequisite.
No ARM deployment, beets migration, mount change or managed-library write was performed; Q15's temporary NFS scratch writes are recorded above.

Merge requires the selected implementation's tests and isolated NixOS/package validation.
Deployment separately requires host-owner approval, hardware checks, credentials/permissions, a test destination, and disabling competing drive handlers/continuous loops.
No push, publication, host rebuild, live import, backfill, or change to the sibling jj change is authorized.
If the chosen delivery needs more than one change, stop and narrow or request `/plan-stack`.

## Watch and rollback

Keep secrets out of the Nix store and preserve the existing separately provisioned beets credentials.
Treat metadata/filenames/cover URLs as untrusted input; validate image downloads and avoid shell interpolation.
The mount guard expects `sec=sys`; the inspected configuration does not establish encrypted NFS transport or server-side export restrictions.
Tailscale being enabled is not proof that this mount uses it.
Network security and measured NFS performance remain storage-owner validation concerns, not excuses to claim an encrypted mount or silently redesign transport here.
A service sandbox must allow required optical-device ioctls and state access without making ripping privileged by default.
Do not expose a writable browser API merely to avoid a terminal.
Q12 defers browser import integration, including its web hosting/authentication scope.

Roll out to an isolated incoming destination first, keeping the old workflow available but never competing for the same drive.
Rollback disables the new detector before restoring the old one and retains pending staging.
Do not undo or duplicate completed beets imports automatically.
The existing Nix runbook owns catalog restore/rollback; this ripper change must not switch catalog owners or restore an older catalog.
Do not remove the catalog ready gate, bypass its lock, force-unmount blocked storage or switch to a soft mount as a retry mechanism.
Crash recovery follows the durable phase/attempt receipts above; it does not guarantee survival of disk loss or an older-state restore.
Retain uncertain jobs for reconciliation rather than claiming exactly-once publication across NFS and a separate importer.

## Review record

The complete revised draft and current question were reread in a separate review pass against the cited code, live configuration, user answers, and `/implement` contract.
The revised plan separates user answers, current local source, live NAS settings, historical server observations, and untested proposals.
It corrects the earlier assumption that `Lainatut/` was the live handoff and that ownership needed to control extraction.
It also records the existing artwork settings that prevent assuming beets already supplies automatic replacement.
ARM review now includes the shared output root and limits of its notification hook, not just its UI.
The user's reported homeslab cutover supersedes Q8 and the old pending-migration premise.
The current module, launcher, shared checks, configuration and runbook were inspected again before revising this draft.
Review corrections include the guarded launcher's UID/self-locking contract, canonical import paths, the separate publisher's need for mount checks, disabled art replacement and hard-NFS blocking.
The user's gain-tool correction led to recovery of the actual remote rsgain configuration and removal of the mistaken loudgain package requirement.
Pinned abcde source and a stubbed invocation check establish that direct configuration works without another patch.
Active references now point to the maintained Nix implementation rather than removed planning documents.
Q10–Q11 select Telegram and separate final import while explicitly permitting a simpler existing web-importer alternative.
The follow-up review distinguishes the built-in library browser from beets-flask's actual GUI importer, and records its missing cover/ownership controls, version pin, copy-only semantics and the host's lifetime-lock mismatch.
Q12 A resolves that trade-off in favor of Telegram review and the existing terminal importer.
The current revision removes the browser-adaptation verification obligation and makes manual import explicit, so implementation cannot invent an unattended importer.
Q13 A selects the standalone coordinator and names it discipline; ARM is now recorded as rejected rather than left as an implementation branch.
Q14 confirms the exact workspace; read-only inspection found it absent, so no existing project tooling was assumed.
A read-only design review added the pre-publication fence, retained receipts, uncertainty handling and separation of blocked NFS work from extraction.
Additional abcde source inspection found structured artwork provenance and the zero-exit-with-errors hazard; completion validation now requires recorded processing evidence.
The follow-up review also required independent drive/Telegram scheduling, atomic worker/cleanup exclusion, exact crash-window tests and mount validation before any probe write.
Those corrections are reflected in the contract and Q15.
Q15's first attempt failed SSH authentication; the authorized retry then completed with cleanup verified.
Its absent-destination `EINVAL` rejects `RENAME_NOREPLACE` on this mount, despite successful collision refusals and flush calls.
A read-only review found reserved-directory publication viable only with explicit cooperative-writer ownership, pinned canonical identity, durable reservation/attempt records and no automatic adoption after interruption.
The subsequent user clarification rejects the need for that guarantee: inbox files may be overwritten and a rerip should be able to replace them.
The current revision removes the reserved-directory proposal, NAS staging and its probe gate rather than adding another workaround.
Local attempt isolation, direct-copy verification and protection of the managed library remain required.
Q17's answer, "SGTM", accepts the recommended replacement policy, including stale files in that same disc-specific inbox directory.
The follow-up review requires source-manifest validation before deletion and exact destination-content verification before local cleanup.
Source inspection also identified the stable-rerip-path interaction with beets incremental/resume history; a per-invocation manual import override is specified, not a catalog/configuration change.
A phone ownership/cover action is not approval to automate the existing interactive metadata importer.
No Nix code was changed.
Runtime evidence is limited to the bounded Q15 syscall/flush/cleanup probe and the local stubbed abcde gain-dispatch check.
Neither establishes real audio parity, crash durability, security or performance.

Q18 corrects the assistant's unapproved Python choice with the user's Go preference.
The revised proposal uses Go standard-library orchestration, SQLite/libudev bindings, CGO-aware Nix packaging and Go tests; none has been built or run yet.
A separate future import WebUI is recommended, preserving the current inbox/manual-import seam without adding speculative integration code.
The plan is ready for final approval; no further current-scope language or WebUI decision is required.
Repository creation and document relocation are now authorized and complete; implementation still requires separate authorization, and runtime acceptance checks remain unrun.
Final approval and `/implement` remain separate steps; discovery is not permission to build or deploy a stack.
