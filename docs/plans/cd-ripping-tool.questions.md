# discipline decisions

Plan: [cd-ripping-tool.md](cd-ripping-tool.md).
Planning one discipline application/package change; no implementation is authorized.
Q10–Q15 and Q17–Q18 are answered and preserved below.
The authorized probe completed and rejected the original no-replace publication method; see the [round 9 result](#round-9-probe-result).
The user's [round 10 clarification](#round-10-resolution) supersedes the reserved-directory proposal: copy directly into a disposable inbox with overwrites allowed.
[Round 11 is resolved](#round-11-resolution): Q17 accepts replacing the contents of the selected disc's inbox directory, including stale files.
[Q18 in round 12](#round-12-resolution) gives Go as the preferred language and asks where a future import WebUI should live.
The plan now uses Go; a separate future WebUI project is recommended without expanding this change.
Final plan approval and implementation authorization remain separate.
The user reports the separate beets/NFS migration complete; its [current implementation/runbook](../../../nix-config/hosts/nixos/homeslab/media/music/README.md) is the integration baseline.
Q12 confirms Telegram review and separate beets import; browser import integration is deferred.
Q13 selects the standalone application and names it `discipline`; Q14 confirms `/Users/oula/gits/discipline`.
The user's subsequent request authorized creating that jj repository and moving these documents there; the relocation is complete.
Rounds 1, 3 and 10 are superseded history; rounds 2, 4, 5, 6, 7, 8, 9, 11 and 12 are answered and preserved below.
Blank answers are not agreement.

## Already established

- NixOS compatibility, audio-CD detection, owned/loaner selection, easy metadata/artwork correction, and existing media-output behavior are required.
- Ripping need not be interactive.
  Your clarification: "It doesn't have to be interactive, as long as fixing the metadata and cover art is made easy. And I guess beets could own that part? The tough part is the owned/loaned division."
- A CLI is negotiable rather than an unconditional requirement.
  Your clarification: "I'm not sure if cli is absolutely mandatory if metadata fixing is easy enough."
  Q4 defines what an acceptable correction workflow looks like; this is not agreement to remove all command-line access.
- Remove the old failed-`abcde` retry loop used to wait for a CD.
- Use zmx rather than tmux when persistent terminal interaction is needed.
- Rsync was preferred for ease/security, not as an end in itself.
  Music is now mounted on homeslab through NFS; the proposed normal handoff uses that mount rather than maintaining an additional SSH transfer path.
  The inspected `sec=sys` mount checks do not prove encrypted transport or server export security.
- Q4/Q7: the existing beets import works well, and separate interfaces are fine initially.
  No new unified WebUI is required.
- Q5: start extraction unattended, then review before final library acceptance.
  Q8's fixed NAS-import premise is superseded by the subsequent compute/storage discussion.
- Q6: owned/loaner is an import classification already handled by beets.
  Later loaner-to-owned reclassification is explicitly out of scope.
- The user now reports beets running on homeslab with NFS-mounted music.
  The inspected Nix configuration keeps catalog/state under `/srv/beets`, music under `/mnt/media/Music`, and nightly catalog backups on the NAS.
  The old NAS shell is no longer an execution dependency.
- Your earlier database clarification: "DB could live on the server as well, as long as it's automatically backed up to NAS too".
  Preserve the existing backup/locking implementation rather than adding a second one.
- Q10 chooses two-way Telegram review for cover replacement and ownership.
  Q11 accepts the separate terminal import, but would consider one-way notifications plus an existing web importer if that is easier than building the bot review.
  Q12 A retains Telegram review and the terminal importer after considering the web-importer findings.
  Browser integration is deferred; do not implement both interfaces.
- Q13 A selects a standalone application around patched `abcde`, named `discipline`, in a dedicated repository.
  Its one implementation change includes the application, Nix package/module, focused tests and operating instructions.
  ARM integration is excluded from that change.
  Q14 confirms `/Users/oula/gits/discipline` as the future implementation workspace, without permission to create/initialize it.
- The existing Nushell/zmx change `zklswqxqpnqszvkrqploqrovprxxqkuw` is an earlier approach, not an implementation prerequisite.
  Your clarification: "That was my initial idea but then I realized I probably want the logic out of my dotfiles and maybe not rely on just shell scripts".
  Keep that change untouched; do not build a second dotfiles-owned ripper.

## Design tree

Q4–Q7 settle unattended extraction, acceptance of the current beets interface, import-owned classification, and separate tools.
Beets now runs on homeslab against the mounted media tree, making Q8's fixed-host alternatives and the pending-migration premise obsolete.
Q9 kept that storage work separate; it is now existing infrastructure, not a second change the ripper may create.
The user rebased and checked out planning change `lnsuznpsnsuunkrqtlpyzzlpswzknkur` and authorized updating these documents.
Q10 selects Telegram; Q11 accepts approval preparing the inbox before final beets metadata import.
Q12 confirms that baseline and defers browser import integration.
Q13 selects discipline as a standalone application; Q14 confirms its exact local repository path.
The plan now proposes the files, dependencies, job lifecycle, recovery commands and publication contract for that workspace.
Q15's completed probe showed the existing NFS mount cannot perform the proposed no-replace publication to a new destination.
Q16 proposes reserving a private job directory instead, with an explicit cooperative-writer assumption and a bounded check.
Do not treat probe permission or successful flush calls as approval of that changed contract.
Do not build both interfaces, automate final metadata acceptance, or change catalog ownership implicitly.
No tool implementation, mount, database migration, or deployment is authorized by this discussion.

<details>
<summary>Round 1, superseded before any answers. Do not answer Q1–Q3.</summary>

## Round 1, superseded

The questions below assumed a mandatory interactive CLI and favored a custom tool too early.
The user's clarifications remove that premise.
All answer fields were blank when this round was superseded; the original questions remain for history.

### Q1. Which direction should this one-change plan take?

Both the existing workflow and ARM use `abcde` for audio CDs.
ARM's inspected audio path is noninteractive and logs output rather than exposing an interactive ripping CLI.
Its music UI does not expose the generic title-selection controls, and the traced path does not provide per-disc owned/loaner routing or your rsync delivery step.
Its standard deployment documentation also does not establish NixOS compatibility.
See the plan for source links and limits of that investigation.

- A. A self-maintained standalone application, not a dotfiles function or merely the same shell script in a new repository.
  Reuse `abcde` for the existing media behavior unless a requirement proves it unsuitable.
  The one-change candidate is the CLI application, isolated jobs, rsync delivery, zmx interaction, and its own Nix package/module.
  Live host deployment and dotfiles caller migration are outside that change.
- B. Adopt ARM and maintain the adaptations needed for the mandatory CLI, per-disc choices, existing output, and rsync.
  First bound those adaptations and their NixOS verification; an ARM WebUI alone does not satisfy CLI parity.
- C. Neither narrow candidate is enough; define a larger replacement including the additional features you require, then narrow it to one change or choose `/plan-stack`.

Recommendation: A.
It preserves the media behavior you already tuned without taking on ARM's video-oriented application and then rebuilding the missing interactive audio workflow.
The application can be non-shell while invoking mature command-line media tools; rewriting the encoder/extractor is unnecessary.
This is not a language choice yet.

Answer:

### Q2. What must happen before anyone attaches, and which optional features belong in the first delivery?

The earlier session allowed detection to create a job that waits for attachment.
Your new request also raises starting extraction immediately while metadata is entered.
Those are different observable behaviors, regardless of the chosen application.

For start timing, choose one:

- A. Detection starts a persistent job automatically, then it waits for owned/loaner and normal metadata/cover prompts before extracting.
- B. Detection starts audio extraction immediately into unclassified temporary output.
  Ownership, metadata, and cover choices may happen during extraction, but no delivery occurs until those choices and processing succeed.
  This needs a different pipeline or proof that the selected backend can separate extraction from finalization.

Also state whether a WebUI and beets integration are required in this first delivery or may be deferred.
An existing ARM UI would not be removed if ARM were selected; the question is whether this change must add or adapt browser controls for your audio workflow.

Recommendation: A, with WebUI and beets deferred for the parity-preserving first change.
If B or either integration is important enough to justify expanding the first change, say so now rather than accepting a design that will fight it later.
A failed concurrency check must return for a decision, not silently downgrade B to A.

Answer:

### Q3. Is loaner versus owned only classification and destination routing?

Today `--loan` puts output under `Lainatut/`; owned albums use the destination root.
Nothing in the script expires loaner files when the physical CD is returned.
It also does not ask on each disc, which the replacement must do.

Proposed contract:

- Every newly inserted disc asks owned, loaner, or skip, with no inherited or timed default.
- Loaner output keeps the `Lainatut/` prefix; owned output keeps the existing layout.
- Neither classification adds automatic expiry, deletion on return, or a separate encoding format.
- Once selected, classification stays with that job through disconnects and retries.
  A future beets import must preserve it rather than infer ownership from an album's identity.
- A loaner and an owned copy of the same album may coexist in their separate trees.

Is that the intended meaning, or do you need a different ownership/retention rule?
This does not yet decide how an existing album in the same tree is replaced or skipped; that is a later collision-policy decision.

Recommendation: accept the proposed contract.
Keep ownership explicit and preserve the existing directory division without inventing lending or retention management.

Answer:

</details>

## Round 2, answered

### Q4. What would make metadata and artwork correction easy enough?

Consider a disc with the wrong release match, a misspelled track title, and an incorrect cover.
Which workflow would you be happy to use?

- A. Browser-based correction is enough; no dedicated ripping CLI is required.
  Find the album, choose another release or edit album/track fields, preview the cover, and replace it using an image file or URL.
  Say whether this must work on a phone, a desktop browser, or both.
- B. A separate desktop music-tagging application is acceptable if opening the ripped album is straightforward and saving changes keeps ownership and the delivered files correct.
- C. A terminal workflow is acceptable if release selection, field edits, and cover replacement are straightforward.
  A WebUI is optional rather than a requirement.
- D. More than one of these is acceptable; list them and your preference.

Beets can retag music and replace embedded art, but that does not by itself provide the complete browser editor in A.
Its built-in web plugin documents browsing/playback and an optional write API, not a complete release/cover-editing workflow.
Likewise, ARM having a WebUI does not establish that its music metadata controls meet this example.
I will verify candidate interfaces against your answer.

Recommendation: D if several approaches genuinely work for you, with a preferred interface and any deal-breaker named.
Do not require two complete interfaces merely to keep options open.

Answer: D.
Honestly Beets import has been working great for me. I haven't needed the artwork fix with that though since so far ripping has been interactive and I always check the cover URL abcde will be using and provide a better one if the initial pick is not good enough. That's gotta be the second hardest thing besides the owned/loaned division.

### Q5. When should the system wait for you?

Choose the default behavior for an inserted audio CD, including a disc that is not recognized automatically.
All options must retain unfinished work when you close the browser or disconnect.

- A. Start ripping without waiting for human input, then hold the album in staging for owned/loaner selection and a quick metadata/cover review before rsync delivery.
  Corrections may happen after extraction; editing while the drive is still reading is not required.
- B. Start ripping without waiting for human input; once you select owned/loaner, automatically deliver recognized albums without a separate review of every album.
  Unrecognized albums wait for review.
  Wrong matches and covers must remain easy to fix after delivery, which also needs a safe way to update remote files and changed paths.
- C. Detect the disc and create a pending job, but wait for your ownership/metadata approval before ripping.
- D. Another sequence; describe where ownership selection, metadata review, and delivery should wait.

If changing metadata during extraction is still a requirement for the first change, say so explicitly.
It is different from letting extraction proceed unattended and fixing tags afterward.

Recommendation: A.
It permits unattended extraction and separates it from review, while avoiding an initial implementation that has to reconcile renamed files already delivered to the NAS.
This is a proposal, not an assumption that every current backend supports it without adaptation.

Answer: A.

### Q6. Is loaner versus owned only classification and destination routing?

Today loaner output goes under `Lainatut/`, while owned output uses the destination root.
The script does not expire files when a borrowed disc is returned.

Proposed contract:

- Each disc job requires an explicit owned/loaner choice before delivery, with no inherited or timed default.
  Q5 determines whether that choice happens before or after extraction.
  You can discard or skip a job instead of classifying it.
- Keep `Lainatut/` and the existing owned layout.
- Do not add automatic expiry, deletion on return, or a different encoding format.
- Preserve classification through metadata correction, cover changes, renaming, disconnects, and retries.
- Allow loaner and owned copies of the same album to coexist in their respective trees.

Does that describe the division you need?
If you later buy a borrowed album, should you be able to reclassify the existing copy as owned, or would you rip the purchased disc as a new job?
Replacing an existing album within the same ownership tree is a later collision-policy decision.

Recommendation: keep classification independent of album metadata and retain the current directory split.
Treat a newly purchased physical disc as a new job initially; request reclassification if it is part of your normal workflow.
The storage mechanism is ours to investigate, not something you need to design.

Answer:
For ripping the division doesn't really matter. It matters when importing albums to my beets library. It tags loaners and owned as such and moves them to the right place in the disc. They end up in separate subdirectories. It's just been the easiest to do the beets import when the abcde output has been already divided based on owned vs loaned.

No need to care about retagging a loaner that becomes owned later. That's up to me and beets how we deal with that. Some CDs might get ripped anew, others just moved. Not in scope of this project though.

### Q7. Is using separate tools acceptable if the handoff is simple?

For example, one tool could detect/rip discs and another could correct metadata/artwork.
This does not assume ARM plus beets already supplies a complete solution.
Whatever is selected must carry ownership across the handoff without manual directory moves or database edits.

- A. Prefer existing tools, even with separate interfaces, provided the new album is easy to find and ownership/delivery remain reliable.
- B. Prefer one interface for job status, owned/loaner selection, metadata/artwork correction, and delivery, even if that means maintaining more custom application code.
- C. Either is acceptable; minimize the amount of code and operational maintenance we own.

Recommendation: A or C unless switching interfaces is itself a usability problem.
First test the fit of existing tools, then plan one bounded integration or standalone-app change.
Do not choose a language, embed libzmx, or commit to building a new WebUI before those checks.
If the chosen experience requires multiple repository changes, we will narrow the scope or ask you to choose `/plan-stack`.

Answer: C
We might want a unified interface later, but even then we should not reinvent the wheel. Separate interfaces is completely fine for the initial implementation.

## Round 2 resolution and verified facts

This section records the pre-migration NAS inspection; current homeslab facts follow round 4.
Your answers preserve the current beets import instead of requiring the ripper to become a library manager.
The ownership distinction belongs at import, and a cover URL you can inspect/replace is an established interaction worth retaining.
A new WebUI and loaner-to-owned conversion are not first-delivery requirements.

Read-only inspection of `spikenology:/var/services/homes/oula/.config/beets/config.yaml` confirmed:

- Beets uses `/volume2/Media/Music/` and `/volume2/Media/beets/library.db` on the NAS.
- Its path templates already begin with `$source/`.
- Imports move files and use incremental/resume behavior; no import was run during investigation.
- Automatic fetchart and embedart are disabled.
  `embedart.ifempty` is true, so a replacement workflow must explicitly handle existing embedded artwork.

A prior server inspection recorded delivery into `Music/incoming/Owned` and `Music/incoming/Loaned`.
That differs from this checkout's older `Lainatut/` arrangement; the plan now distinguishes them rather than treating the local defaults as authoritative.
The current importer commands and exact assignment of `source` remain to be inspected.

## Round 3, superseded before an answer

Q8 assumed preserving the beets workflow meant keeping its execution on the NAS.
The user's later clarification removes that constraint; do not answer this historical round.

### Q8. Does review have to happen before upload, or before acceptance into the music library?

Q5's option A said review happens before rsync.
Your answers also favor the existing beets import, which runs on the NAS after the server uploads files.
I should not silently move that import or reinterpret the approval gate.
Which boundary do you want?

- A. Upload completed rips to a NAS inbox first, then choose owned/loaner and review metadata/artwork there before accepting the album into the managed library.
  Unreviewed files may exist in the inbox but are not final library albums.
  This removes ownership from the extractor entirely; the incoming-review/import step must carry it into the existing `source` field.
- B. No upload before owned/loaner, metadata, and cover review are complete on homeslab.
  This keeps Q5's literal ordering, but full metadata review must work before the existing NAS import and cannot just be assumed to happen through that import.
- C. Keep the current division of work: on homeslab approve owned/loaner and the cover URL after automatic extraction, then rsync into the classified NAS inbox; do final metadata matching/correction with the existing NAS beets import.
  This preserves convenient pre-divided incoming folders without making ownership part of extraction or creating a second beets catalog.
  It explicitly relaxes Q5's pre-upload metadata review, not its ownership/cover review.

Recommendation: C for the smallest change to the workflow you say works well.
Choose A if you prefer all human review on the NAS and are happy for unreviewed rips to be uploaded to its inbox.
Neither choice permits automatic library acceptance without the selected review, and neither adds library reclassification or a custom unified UI.
After this answer, I can compare ARM integration and a standalone component against the actual handoff instead of their UI feature lists.

Answer:

## Storage clarification and recommendation, historical

This was a proposal before the independent Nix implementation.
It is not a current instruction to choose SMB or migrate the catalog again.
You are open to beets running on homeslab while the NAS stores music, rather than letting the data location dictate the execution host.
You accept a live catalog on homeslab if it is automatically backed up to the NAS.
Those statements do not yet authorize migration or select the first implementation change.

Recommended arrangement:

- homeslab runs ripping and beets, with local staging and the live SQLite catalog.
- The NAS stores the music behind an authenticated encrypted mount and receives automatic consistent catalog snapshots.
- Keep the existing ownership field, library paths, and familiar beets import behavior.
- Evaluate SMB3 encryption for music access; do not put the live SQLite database on the network share.
- Use SQLite-aware snapshots for backups, not ordinary rsync of a database while it is being written.

At that point the separate `nix-config/docs/plans/beets-homeslab.md` owned this proposal and cited SQLite's network-filesystem warnings and Linux SMB encryption options.
That planning file is no longer present in the current Nix checkout; the maintained [music runbook](../../../nix-config/hosts/nixos/homeslab/media/music/README.md) now documents the implemented NFS/local-catalog arrangement.
Mount/plugin compatibility and actual importer performance remain untested.
We will measure representative imports/artwork changes on disposable copies rather than infer performance from playback bandwidth or a bulk copy test.
Backup timing, retention, restoration, missing-mount handling, and migration rollback belong in the selected change's contract before implementation.

## Round 4, answered

### Q9. Which scope should we plan next?

There are now two independently useful deliverables: safely running beets on homeslab against NAS storage, and replacing the ripping orchestration.
Combining both in a supposed one-change plan would hide the storage/catalog migration and its validation.
Which scope do you want?

- A. Keep `/plan` to one change and plan the beets-on-homeslab setup first.
  Include secure media access, local catalog configuration, automatic NAS backups, compatibility/restore checks, and the migration runbook.
  Leave the existing ripping commands unchanged for now.
  We can plan the ripper replacement against the verified setup afterward.
- B. Keep this plan focused on the ripper replacement and defer beets relocation.
  Use the existing NAS import as the initial integration target and explicitly leave the mount/catalog migration for another task.
- C. Choose `/plan-stack` for the complete direction.
  Plan storage/library-management migration and ripping automation as separate dependent changes, without implementing either.

Recommendation: A if you want to retain the original one-change constraint; C if you want the whole revised workflow planned together.
In either case, acceptance of the architecture does not authorize mounting shares, copying the live catalog, changing NAS permissions, or deploying services.

Answer: B.
I think we should maybe do the NAS thing first but that's a problem for nix repo, not this.
Can you write a separate plan for that so we could start working on it while we still figure out requirements for this change?

## Round 4 resolution

Historical resolution, with its initial importer target superseded by the completed migration below.
Keep this document about the ripper, with the existing NAS importer as its initial integration target.
Plan beets on homeslab separately in `nix-config/docs/plans/beets-homeslab.md`.
The user then confirmed `/Users/oula/gits/nix-config` as the plan's repository and continuation in dotfiles working change `ysuvxxrr`.
That confirmation authorizes planning edits, not mounts, migration, or deployment.

The Nix work can proceed while ripper requirements remain open, and may deploy first.
If it does, inspect the resulting importer and revise this handoff before implementation rather than building two transport/import paths.
Neither implementation is an implicit prerequisite the other change may create.
The ripper backend and exact review/upload/acceptance sequence remain unresolved.

## Completed homeslab migration and new phone-review proposal

The user reports beets now running on homeslab and media mounted there through NFS.
Read-only inspection of `nix-config/hosts/nixos/homeslab/media/music/` confirmed the intended contracts:

- `/srv/beets/library.db` and `state.pickle` are local; music is `/mnt/media/Music`.
- The managed `beet` command checks the configured mount and ready gates and takes the same lock as nightly backups.
  Do not bypass that launcher or hold its lock throughout ripping/review.
- Existing imports set `source=Owned` or `source=Loaned` and read the corresponding `/mnt/media/Music/incoming/` directory.
- Artwork auto-fetch/embedding remain disabled and `embedart.ifempty` remains true; replacement still needs deliberate handling.
- NFS is hard-mounted and the guard expects `sec=sys`; extraction should remain local and publication must retain output during an outage.
  This source inspection is not a new runtime, network-security or performance test.

The user then proposed a phone notification with the cover image/link, ideally accepting replacement art and ownership through Telegram or Discord.
The [plan](cd-ripping-tool.md#phone-interaction-feasibility) records the relevant API evidence.
Telegram can use long polling; Discord can use its outbound Gateway.
Neither necessarily requires exposing homeslab publicly, but two-way replies require an application, not merely a posting webhook.
No bot was created, no token provisioned, and no message sent.

## Round 5, answered

### Q10. Which phone interaction belongs in the first version?

Every option sends a notification only after a successful rip, with the candidate cover, source URL when available, album/job identity, and a clear "ripped, awaiting review" status.
It must not claim the album is already imported into beets.

- A. Notification only for now.
  Inspect the cover on the phone, then use the homeslab review step to change it or choose ownership.
  State Telegram or Discord as your preferred destination.
- B. Two-way Telegram review.
  Reply to the album's message with a replacement image URL or an image/file; use buttons for owned/loaner and cover approval.
  Use a single private chat and long polling, not a new public web endpoint.
- C. Two-way Discord review.
  The same user-facing capabilities, implemented as one bot/application with the required reply/attachment handling, not just a webhook.

Recommendation: B if you already use Telegram and want the phone to replace the routine review terminal.
Choose C if Discord is where you will actually use it; we should implement one provider, not both.
Choose A if receiving the alert is enough for the first change.
Two-way review adds authenticated job updates and retry/restart handling, so it is a real scope choice rather than a free notification setting.
Only album information, cover imagery and links go to the chosen provider, not your audio files or catalog.

Answer: B

### Q11. Is phone approval preparing the inbox enough, with metadata import still in beets?

Recommended first-version flow:

1. Extract locally without waiting for input.
2. Send the completion notification.
3. Review the cover and choose owned/loaner through the selected interface.
4. Publish the reviewed complete job into the matching mounted inbox.
5. Later use your existing interactive `beet im --set source=Owned` or `source=Loaned` workflow for final metadata/release matching and library acceptance.

This permits an album in the inbox before final metadata approval, but not automatic acceptance into the managed library.
It replaces the old SSH transfer, not your successful beets tagging interaction.

For two-way review, proposed buttons are "Approve owned" and "Approve loaner": one explicit action accepts the displayed cover and chooses ownership.
A replacement reply produces an updated preview and still needs approval.
No reply leaves the job pending, with no timed/default ownership choice.
Replies after publication do not silently change the library.
For notification-only delivery, the local review step supplies that approval instead.

- A. Yes: phone/local approval prepares the inbox; keep final metadata import as a separate terminal step.
- B. No: I want the phone interaction to complete metadata review and library import too.
  That needs a defined release/track/duplicate-selection workflow, not merely launching today's interactive importer in the background.
- C. Another boundary or approval policy; describe it.

Recommendation: A for the first version.
It solves phone notification, cover review and ownership without rebuilding beets' metadata UI or holding its lock while waiting for a user to attach.
If B makes the complete requirement too large for one reviewable change, we must narrow the first delivery or choose `/plan-stack`, not silently omit part of it.

Answer:
A. But I started wondering: does beets offer a web ui for imports? Or has someone built one if it's not built in? Because in that case we could maybe utilize that for everything: checking/swapping cover image, determining the own/loan status, checking the rest of the metadata, and then move the files to the right location. If it's easier to implement than two-way Telegram review then the notifications wouldn't have to be two-way

## Round 5 resolution and web-importer findings

Q10 selects Telegram; Q11 accepts separate final import but asks whether an existing importer could avoid custom review code.
Read-only documentation/source inspection found:

- Built-in beets `web` browses/plays the library and optionally edits existing items; it does not offer an import/release-selection workflow.
- **beets-flask** is a real web importer.
  It previews candidate releases, album/track differences and artwork, supports alternate release selection, and can wait for manual import approval.
  However, the inspected UI has no cover URL/upload replacement or per-album owned/loaner selector, and is not an arbitrary track-field editor.
  It supports copy imports only and pins beets 2.5.1, unlike the current homeslab 2.14.1 setup with move imports.
- beets-flask opens beets' library through Python rather than the guarded launcher.
  Wrapping its whole server in the current launcher would instead keep the backup/import lock held indefinitely.
  Its optional web terminal uses tmux; it is not a ready workaround for missing review controls under your zmx requirement.
- Beetiful is an archived command-builder/config-editor project, not this review workflow.
  Beetle is a frontend for the built-in library API, not a full importer.

See the [plan's source citations and compatibility limits](cd-ripping-tool.md#existing-web-importer-investigation-after-q11).
beets-flask has recent upstream activity; it is not rejected as abandoned.
The problem is the work still needed for your specific review and importer contracts.
No complete, simpler drop-in was established, and none was installed or tested against a catalog.

## Round 6, answered

### Q12. Keep Telegram review for the first version, or prioritize web-importer integration despite those gaps?

- A. Keep Q10 B and Q11 A: Telegram handles cover/ownership approval, then the existing terminal beets workflow handles final metadata import.
  Defer browser import integration; do not maintain two review interfaces.
- B. Browser review is valuable enough to investigate adaptation further, even though it is not a simpler drop-in.
  Prioritize a bounded web-importer integration plan and one-way notifications instead of assuming the two-way bot remains required.
  Resolve cover replacement, ownership, copy/move behavior, version compatibility, access control and guarded catalog operations before selecting it.
  If that needs a separate importer change, narrow the first delivery or explicitly choose `/plan-stack`; do not silently expand this ripper change.

Recommendation: A.
The inspected web importer still needs the artwork and ownership features the Telegram review would supply, and adds catalog/backup integration work.
Keeping your working importer avoids taking that on just to remove its terminal step.
This is a source-based scope recommendation, not a measured implementation-cost claim.

Answer: A

## Round 6 resolution

Two-way Telegram review and the existing terminal beets import are settled.
The application prepares the classified inbox; it does not run the final importer unattended.
Browser import integration is deferred, and its compatibility work is not a prerequisite for this change.

## Round 7, answered

### Q13. Which application and repository should own this workflow?

The review interface is settled, so we can choose the ripper without assuming that a WebUI is a benefit.
Both directions still need isolated jobs, safe artwork replacement, persistent Telegram review and safe NFS publication.
Neither may become another dotfiles-owned shell wrapper.

- A. A small standalone application around the existing patched `abcde`, in its own repository.
  It owns automatic disc handling, per-job state/staging, Telegram review and publication to the classified inbox.
  Reuse existing extraction/encoding/gain tools and leave metadata matching, catalog ownership and final import to beets.
  The one implementation change includes the application, its Nix package/module, focused tests and operating instructions.
  Homeslab activation and retiring old dotfiles callers are separate rollout steps, not additional commits silently included in this plan.
- B. ARM integration in an explicitly chosen repository.
  Reuse ARM's drive detection and status UI, then adapt its audio path for isolated jobs and the same Telegram/inbox contract.
  The inspected ARM notification hook does not supply a structured completed-album handoff, and its container does not inherit the Nix `abcde` patch.
  Those gaps must be bounded before promising one integration change; this option is not permission to maintain an ARM fork plus a separate application.

Recommendation: A, in a dedicated repository outside dotfiles and `nix-config`.
The custom work is now job/review/publication coordination either way; ARM adds a service and integration boundary without replacing those parts.
This does not mean rewriting CD extraction or building a metadata editor.

Include the intended local repository path in your answer.
For A, `/Users/oula/gits/cd-ripper` would be a reasonable new location, or name an existing repository you prefer.
Choosing a path authorizes targeting the next planning pass there, not creating/initializing a repository, writing implementation code or deploying a service.
These planning documents remain here unless you ask to move them.

Answer: A
Let's call it discipline because it meticulously rips your discs.

## Round 7 resolution

The application is `discipline`, using patched `abcde` with its own job/review/publication logic and Nix package/module.
ARM and browser import integration are excluded from the selected change.
The project name is settled; Q13 did not include a local repository path.

## Round 8, answered

### Q14. Should the implementation workspace be `/Users/oula/gits/discipline`?

Recommendation: yes, matching the chosen name in your existing projects directory.
Confirm that path or give another one.
This selects the planning target only; it does not authorize creating/initializing a repository, moving these documents, implementing code or deploying anything.

Answer: Yes.

## Round 8 resolution and technical review

The implementation workspace is confirmed as `/Users/oula/gits/discipline`.
Read-only inspection found it absent; no directory or repository was created.
The [plan](cd-ripping-tool.md#proposed-implementation-contract) now specifies a proposed Python application, Nix package/module, separate NFS publisher, job receipts and recovery commands.
These are proposed technical choices for final plan approval, not additional implementation authorization.

Two findings matter for correctness:

- The inspected abcde source can exit zero after recording processing errors.
  Discipline must verify every expected audio-processing result and retain scratch/status until that check, rather than treating process exit as success.
- The proposed publication must expose a complete album without overwriting an existing directory.
  Ordinary rename can overwrite an empty directory; checking existence first is racy.
  Linux `RENAME_NOREPLACE` needs filesystem support, and an NFS error can still follow a successful remote rename.
  We have not tested this mount, so the plan must not assume support or call the safe publication problem solved.

Crash recovery also needs a durable attempt record before publication.
If beets consumes the inbox after publication but before a success receipt is saved, absence cannot tell us whether publication happened.
Such jobs stay uncertain for manual reconciliation; they are never automatically republished.
No hardware, NFS or catalog experiments ran during this review.

## Round 9, answered

### Q15. May I run a disposable publication-capability probe on homeslab's existing NFS mount?

Recommendation: yes, before approving an implementation that might depend on an unsupported filesystem operation.
The probe would:

- Run as the existing UID 1000 user on homeslab, using existing tools only.
- Before any write, verify the visible `/mnt/media` mount is `spikenology.localdomain:/volume2/Media`, NFS/NFS4, writable, hard-mounted and `sec=sys`, following the existing guard's readiness checks.
  Pin subsequent operations to the validated mounted directory; if absent, wrong, read-only or symlinked unexpectedly, stop without creating anything on the uncovered local path.
- Create one exclusively named, hidden scratch directory beneath `/mnt/media/Music/.discipline-plan-probe-<random>`, outside both classified inboxes.
- Use only tiny dummy files/directories to test atomic `RENAME_NOREPLACE` publication and refusal to overwrite empty/nonempty destinations, then check the required flush operations.
- Record exact results and remove only the objects it created if their identity is still known.
  It would not touch music files, beets catalog/state, configuration, services or the new application repository.
- Stop and report if access/tools are unavailable; do not install anything, change permissions, remount storage or use a weaker rename fallback.
  Because NFS is hard-mounted, an operation may block; that is not permission to force-unmount it or claim cleanup succeeded.

A successful probe establishes basic operation support, not crash/power-loss guarantees.
If unsupported, I will revise the publication design before asking for final plan approval; I will not silently change transport or import workflow.
You may decline and leave this plan blocked, or prefer a read-only publication redesign first.

Answer: Yes

## Round 9 first probe attempt, historical

The user also explicitly confirmed permission in chat: "Yes, I did."
Read-only preflight connected to `spike@homeslab` as UID 1000 and found Python, findmnt, the storage-readiness marker and the expected NFS4 mount/source.
The next SSH connection, carrying the scratch probe, failed before remote execution:

```text
sign_and_send_pubkey: signing failed ... agent refused operation
Permission denied (publickey).
```

SSH exited 255; the probe script and its descriptor-pinning guard never ran.
No scratch directory was created and no music, catalog, configuration or service was modified.
That first attempt supplied no rename/flush capability result.
The user then restored SSH-agent signing and requested the retry recorded below.
No authentication bypass, identity change or installation was used.

## Round 9 probe result

The authorized retry ran at `2026-10-27T07:59:34Z`, as UID 1000 on homeslab kernel `6.18.54`, with the expected NFS 4.1 mount and every pre-write guard intact.
Scratch was `/mnt/media/Music/.discipline-plan-probe-83dc7c4c62954f58941021a48d7a6903`.

- `RENAME_NOREPLACE` to an absent destination returned `EINVAL`; the source stayed intact and the destination remained absent.
- Existing empty and populated destinations returned `EEXIST`; their identities and dummy contents were preserved.
- All tested file/directory/parent flush calls succeeded.
- Cleanup verified removal of every created object and absence of the scratch directory.

The method cannot publish a new album on this mount; collision refusal alone does not make it usable.
These observations do not prove power-loss durability.
No real music, beets catalog/state, configuration or service was changed.
No second publication design has been runtime-tested.

## Round 10, superseded question

The proposal below was not selected; the subsequent chat clarification replaces it with direct copying and permitted overwrites.
No Q16 scratch check was authorized or run.

### Q16. Use an exclusively reserved job directory, and verify it with one scratch-only check?

Proposed replacement, not an automatic fallback:

1. Copy and verify the complete approved album in hidden staging outside the import roots.
2. Exclusively create a fresh private `incoming/<Owned|Loaned>/discipline-<UUID>` directory, refusing any pre-existing entry.
3. Move the complete staged album into that directory's `album` child with ordinary atomic rename, after checking the pinned directory's identity and ownership.
4. Keep durable receipts; uncertain outcomes require manual reconciliation and never automatic republication.

This relies on discipline being the only writer creating entries inside its reserved job folders before publication.
Beets may consume published files, but same-user/admin processes and other writers to the shared inbox roots must not create, move or replace these reserved folders or their contents beforehand.
Private permissions and pinned directory handles do not prevent every such interference.
This is a narrower guarantee than filesystem-enforced no-overwrite against arbitrary concurrent writers.
Tiny hidden reservation markers/folders remain after import for manual housekeeping; automatic remote cleanup is out of scope.

- A. Accept that ownership contract and authorize one scratch-only filesystem check of it.
- B. Do not accept it; keep implementation blocked and investigate another publication design without further writes.

The A check would reuse Q15's UID, mount guards, existing-tools-only and identity-checked cleanup restrictions.
It would operate only inside one new hidden `.discipline-plan-probe-<random>` directory outside the real inboxes, with tiny dummy files/directories and fixture symlinks.
It would verify exclusive directory creation, private owner/mode, refusal of pre-existing/conflicting entries, pinned identity, and complete-directory promotion/flush operations.
It would not invoke beets, touch real inbox/library data, alter permissions on existing directories, create the application repository or implement discipline.
The existing hard-NFS blocking risk still applies; no force-unmount or automatic competing retry.
If you accept the design but not this check, say so explicitly.

Recommendation: A for this single-publisher homeslab workflow.
It keeps NFS and the current importer, and makes the ownership assumption explicit instead of pretending a pre-rename existence check is atomic.
Final plan approval and implementation remain separate.

Answer:

## Round 10 resolution

The user clarified in chat, verbatim:

> Why make it two stage? Current implementation lets abcde write on the server disc (currently ~/abcde dir or something like that, in the future should probably be /srv/abcde) and then move the files into beets inbox after abcde is done. The moving here is unconditional. I think it's fine if it overwrites some files, inbox should not be considered a safe location. We even might want it to overwrite files in case a ripping fails somehow and we need to redo it. Just make sure abcde outputs the music into unique enough dir (e.g. musicbraiz id).

Use local extraction followed by direct copy into a disc-specific classified inbox directory, with overwrites allowed.
Keep separate local attempts and the source until transfer succeeds; do not add NAS-side album staging, reservations or no-replace operations.
The inbox is disposable, but the managed library and other discs remain outside the overwrite scope.
The destination is not ready for manual import while a copy is incomplete.
Stable disc identity must not depend on album title or a release ID shared by several discs.
The plan proposes a deterministic TOC fingerprint for the inbox directory and a separate UUID for each local attempt.
An intentional rerip needs a manual beets invocation that does not skip its reused path through incremental/resume history.
Q16's two options and further test are superseded, not implicitly approved.

## Round 11, current question

### Q17. When replacing a disc's inbox contents, may discipline also remove stale files in that same directory?

Overwriting matching filenames is already accepted.
If a rerip changes tags or naming, however, its new files can have different names and leave the old tracks alongside them.

- A. Replace the contents of this one disc's inbox directory, including removing files absent from the new rip.
- B. Only overwrite matching files; leave extras for manual cleanup before importing.

Recommendation: A, restricted to `incoming/<Owned|Loaned>/<disc-key>/`.
Never delete the category root, another disc's files, or anything in the managed library.
A failed transfer keeps the local rip and leaves the inbox marked incomplete in discipline.
Manual import must wait for transfer completion.
This is a planning decision, not permission to delete real files, run another remote test, or implement the application.

Answer: SGTM

## Round 11 resolution

"SGTM" accepts the recommended A: replace the contents of the selected disc's inbox directory, including removing stale files absent from the new rip.
Deletion is restricted to `incoming/<Owned|Loaned>/<disc-key>/`; other discs, category roots and the managed library remain untouched.
Validate the complete local source before changing the destination, retain it on failure, and wait for successful transfer verification before importing or cleaning the local attempt.
This settles the final product decision, not permission to operate on real files, initialize the repository, implement or deploy.
The plan is ready for final approval; its runtime acceptance checks remain unrun.

## Gain configuration clarification

The user corrected the proposed loudgain dependency, verbatim:

> Not sure how I did on the old machine (maybe by making a symlink) but there I used rsgain instead of loudgain. loudgain hasn't been updated in a while whereas rsgain was updated just last month. The config even makes note that we set options so that rsgain behaves like loudgain used to. It's hacky like that because iirc abcde got confused when I tried to make it call rsgain directly. Maybe we should patch that in abcde as well.

Historical SSH captures confirm `MP3GAIN=rsgain` with `MP3GAINOPTS="custom --album --tagmode=i"` on both the old server and restored homeslab.
Use that deployed configuration as the gain baseline, rather than this checkout's older loudgain settings.
The pinned abcde 2.9.3 source and a local stubbed invocation check support direct configuration; no gain-tool patch or executable-name shim is currently needed.
Real rsgain output and playback remain to be verified.
This corrects factual evidence and implementation details, not authorization to edit live configuration or begin implementation.

## Round 12, current question

The user challenged the unapproved language choice, verbatim:

> Why use python? I don't think we established that as the language of choise

### Q18. Which language should discipline be implemented in?

Python was the assistant's proposal, not a decision made in the earlier rounds.
Its rationale was built-in SQLite/subprocess support and available Linux-device/HTTP bindings for coordinating external media tools.
Neither abcde nor the separate manual beets import requires the coordinator to be Python.
Go, Rust or another preferred language can be considered without changing the agreed workflow.

Tentative recommendation: Python for the small coordinator, unless you prefer maintaining another language.
Your preference should decide this before choosing dependencies or implementing the proposed file layout.
A short comparison can precede the choice if useful; no replacement language is assumed.

Answer: I'm partial to Go. Rust could be interesting but maybe not the best fit for this kind of orchestrator work.
One thing to consider: we probably want to have the custom import WebUI for beets at some point. Should that live in this tool or be a separate project?

## Round 12 resolution

Use Go as the preferred language for the current plan; Rust is not an additional implementation branch.
Replace the Python-specific paths, libraries and tests with the Go proposal, including explicit SQLite/libudev dependencies and Linux/Nix validation.

Recommendation for the future WebUI: a separate application/project that owns beets import review, including music not produced by discipline.
Discipline owns ripping, its cover/ownership review and delivery to the classified inbox; it does not own the catalog or final import.
The two applications can share a host without sharing a language, process or database.
The future UI must plan guarded catalog access and coordination with incomplete inbox copies; no speculative interface or shared SDK belongs in this change.
This recommendation is not a user decision to create a second project now.
The existing Q12 deferral remains in force, so future UI details do not block the ripper plan.
Final plan approval, repository creation and implementation remain separately authorized steps.

## Repository relocation

The user subsequently requested, verbatim:

> Please create a new jj repo under gits/discipline and move the questions and plan there

The jj repository at `/Users/oula/gits/discipline` is initialized.
The plan, questions and linked glossary were moved there, preserving historical answers and updating source links.
Earlier statements that repository creation was unauthorized describe the permissions at that point in the discussion, not the current status.
This request does not authorize application implementation, deployment or a remote repository.

