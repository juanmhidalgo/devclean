# devclean v1 (Linux)

## Decisions — devclean-v1

- **[repo: devclean · step 0]** Poetry venvs are classified `manual` (never auto-deleted) until a proof-of-gone rule is defined; this fills the missing "Assumed" reference in the categories table.
  **Because:** missing evidence must not become a deletion (Boundaries); the user confirmed the plan's recommendation.
  **Binds:** steps 18, 24 of the v1 plan; any future venvs collector change
- **[repo: devclean · step 0]** Under `--quiet`, a skipped collector counts as a failed step and triggers the notification (AC-39).
  **Because:** otherwise a silently broken timer never alerts.
  **Binds:** step 30, 34
- **[repo: devclean · step 0]** Contract choices confirmed: BurntSushi/toml; fake binaries generated at test time by `testenv.FakeBin` (not committed under `testdata/bin/`); `history.json` `{version:1, entries{"<category>:<key>"}, walked}` with `history.json.lock`; separate `clean.lock`; JSON `schema_version` integer `1` with the step-27 fields, additive fields without a bump; `clean --json` is non-interactive; exit 1 emits no JSON document.
  **Because:** the spec left these unspecified or marked them "Ask first"; the user approved the plan's Key Decisions.
  **Binds:** steps 2, 9, 11–13, 17, 25, 27, 34 and any consumer of the JSON output
- **[repo: devclean · step 13]** Pruning is persisted only through `history.Save(path, h, now, prune ...Coverage)`, which applies `Prune` to the merged on-disk + in-memory history under the history lock. Each collector reports a `Coverage{Category, Roots, Unreadable, Complete, Seen}` where `Seen` holds full `"<category>:<key>"` keys, and incomplete coverage prunes nothing in any category.
  **Because:** a merge-only save resurrects pruned entries from disk, and pruning outside the lock would race with `observe`.
  **Binds:** steps 16–20 (collectors fill Coverage), 34 (`clean`) and 35 (`observe`) must pass their coverages to `Save`
- **[repo: devclean · step 17]** Collectors emit `collect.Observation{Key "<category>:<key>", At, FirstSeenOnly}`. An image with a container yields a normal observation, which bumps `last_used`. An unused image with no history entry yields `FirstSeenOnly:true`, which must set `first_seen` only and never bump `last_used`.
  **Because:** AC-17 makes `first_seen` the last use of a never-used image, so recording it as use would keep it from ever going stale.
  **Binds:** step 35 (`observe`) and step 34 (`clean`) when they turn observations into history entries
- **[repo: devclean · step 24]** Re-validation is supplied by collectors through `collect.Result.Revalidators map[Candidate.Path]func(ctx) classify.Decision`. A candidate qualifies only if the revalidated tier equals the original tier. A candidate with no revalidator is skipped ("no revalidation"), except a garbage action whose `ReclaimCmd` is set (builder prune, `uv cache prune`, `pre-commit gc`).
  **Because:** this keeps `Result.Candidates` unchanged, and treating a missing revalidator as skip errs toward never deleting.
  **Binds:** steps 32/34 must merge every collector's `Revalidators` into the map passed to `remove.NewExecutor`; any new collector that produces deletable items must register a revalidator
- **[repo: devclean · step 27]** In JSON v1 each candidate carries exactly one of `path` (filesystem), `image_id` (docker image), `volume` (docker volume name) or `action` (a reclaim action such as `docker builder prune`). `command` appears whenever there is a reclaim command, `index` is set on stale items (1-based, matching the prompt), and `outcome_reason`, `notices[]`, and a null `last_used`/`last_used_source` when unknown are all additive. `candidates`, `skipped`, `warnings` and `notices` are always arrays, and `freed_bytes_by_filesystem` appears only for `clean`.
  **Because:** these fill details the step-27 field list left open, without a version bump.
  **Binds:** any script that consumes `--json` output; future changes must stay additive or bump `schema_version`
- **[repo: devclean · step 36]** The scheduled clean job runs `devclean clean --quiet` without `--yes`. Unattended runs therefore delete only garbage and caches on filesystems under pressure, and stale items are skipped with a notice. `--report-only` installs `devclean report` instead.
  **Because:** AC-37 is silent on flags, and `--yes` would delete every stale item with no one watching.
  **Binds:** the schedule units. Unattended stale deletion would need an explicit opt-in added later.

## Pending QA — devclean-v1

QA items. Ticked ones were verified on the author's machine (2026-10-06).

### Happy path
- [ ] `devclean` on the dev machine prints all four tiers with sizes and changes nothing (compare `docker images` and `df` before/after).
- [x] `devclean clean --dry-run` lists the same items `clean` then deletes.
- [ ] `devclean clean` removes dangling images and an image labelled `devclean.ephemeral=true` with no container.
- [ ] Selecting `1,3` at the stale prompt deletes exactly items 1 and 3.
- [ ] Reported freed space matches the `df` difference.
- [ ] `--only docker` finishes without walking `$HOME` (noticeably faster).
- [ ] `--json` output parses with `jq` and contains `schema_version`.
- [x] `devclean schedule install --report-only` creates a user timer; `systemctl --user list-timers` shows it; the job runs and logs a report.
- [ ] `schedule uninstall` removes the units.
- [ ] A pipenv venv for a deleted worktree is removed as garbage.
- [ ] A notification arrives through `notify_command` with a body starting with `- `.
### Edge cases
- [ ] A venv untouched for months but run yesterday is not stale (atime signal).
- [ ] Opening an IDE that scans all venvs does not mark them used (scan burst).
- [ ] A project in a non-git dir with an old `node_modules` is not listed as stale.
- [ ] A `target/` without `Cargo.toml` is not listed.
- [ ] A symlink inside a scan root pointing elsewhere is not followed.
- [ ] Go module cache (read-only dirs) deletes cleanly; parent dir mode unchanged.
- [ ] Running `observe` while `clean` runs: both finish, history keeps both runs' entries.
- [ ] Project restored between `report` and `clean`: its artifact is skipped with a reason.
- [ ] A `noatime` mount produces one warning.
- [x] Below 85% disk, caches are listed but not deleted by `clean`.
### Empty states
- [ ] Clean machine (nothing to reclaim): report says "nothing to reclaim" per tier, exit 0.
- [ ] No Docker installed: `docker` shown as skipped with reason, exit 2, other categories work.
- [x] No config file: defaults used, no warning.
- [x] No history file yet (first run): classification works; file created after `observe`.
- [ ] Stale prompt answered `none`: nothing deleted, exit 0.
- [ ] Empty watch list: `watch` category shows nothing, no error.
### Error states
- [ ] Running as root exits 1 before scanning (AC-10).
- [ ] Invalid config key exits 1 naming key and file (AC-36).
- [ ] Second concurrent `clean` exits 1 (AC-29).
- [ ] Docker daemon stopped: history entries for images are not pruned (AC-24).
- [ ] Corrupt history file: reported, moved aside, run continues with stateless behaviour (AC-22).
- [ ] Image used by a stopped container: `rm` fails, reason shown, exit 2 (AC-28).
- [ ] npm behind a broken mise shim: reported skipped with status/output (AC-32).
- [ ] `notify_command` exits non-zero: logged, exit 2 (AC-38).
- [ ] Non-TTY `clean` without `--yes`: no stale deletions, message printed (AC-6).
- [ ] darwin binary exits "unsupported platform" (AC-40).
- [ ] A venv with missing `.project` is in `manual`, never deleted (AC-26).
