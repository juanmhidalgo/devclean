# Lessons from the prototype

Before this project there was a working prototype: a single-file Python script
that cleaned a developer laptop on a weekly timer. It was used against a real
machine (a 380 GB disk at 92%), reviewed twice by independent reviewers, and
every lesson below comes from something that actually happened — a measurement,
a bug a test caught, or a review finding that was verified.

This document is input for the spec, not the spec. Where the prototype made a
choice that only fit one machine, it says so.

## What fills a developer disk

Measured on the machine the prototype ran on, in order of size:

| Source | Size | Reclaimable without regret |
|---|---|---|
| A VM disk image | 59 GB | none — kept on purpose |
| Docker images | 34 GB | ~27 GB had no container using them |
| `node_modules`, `.venv`, `.tox`, … across ~130 project dirs | 13.6 GB | the ones in repos unused for months |
| Docker volumes | 15 GB | **unknown** — some held databases |
| pipenv venvs | 9.3 GB | 1.9 GB belonged to projects that no longer existed |
| Package caches (uv, npm, yarn, go, pip, pipenv, pre-commit) | ~12 GB | most, at the cost of re-downloading |
| Old snap revisions, journald | ~5 GB | all, but needs root |

Two patterns stood out:

- **One-off verification images** (built to test a PR, never removed) were
  the largest *recurring* leak — about 1 GB each, dozens of them.
- **Agent/worktree workflows leave a venv behind every time.** Tools that
  create a git worktree, install dependencies in it and delete the worktree
  leave the venv in the package manager's central venv dir, pointing at a path
  that no longer exists. 31 of 60 venvs were in that state.

## Tiers: sort by the cost of being wrong

The design that held up was four tiers, decided by what a wrong deletion costs:

1. **garbage** — nothing references it. Deleted every time, unattended.
   Dangling images, images with an "ephemeral" label and no container, unused
   build cache, venvs whose recorded project dir is gone, tool-native prune
   commands (`uv cache prune`, `pre-commit gc`).
2. **caches** — only costs re-downloading. Deleted unattended, **but only
   under disk pressure** (default: disk ≥ 85%). Wiping them on a schedule just
   re-downloads the same packages next week; they are the reserve, not the
   garbage.
3. **stale** — regenerable, but costs a rebuild or reinstall. Never deleted
   without showing the list and asking.
4. **manual** — state, or needs root. Reported with the exact command to run;
   the tool never deletes it. Docker volumes, VM disks, system logs, OS package
   leftovers, and any cache other software reads live.

Requests that came up while using it:

- **Select categories when cleaning** (`--only docker`). Also makes runs faster:
  each category is one scan, and skipping the project walk turns ~11 s into ~3 s.
- **A report mode that deletes nothing** must be the default.
- **A dry run** for every destructive command.

## Deciding that something is unused

This was the hardest part and where most bugs were.

- **Git activity is a good signal for project artifacts**: last commit, last
  checkout (`.git/HEAD` mtime), last index write (`.git/index` mtime — `git
  status` and every editor polling it rewrite it).
- **The artifact's own mtime** catches a fresh install with no commit.
- **But neither catches "installed 200 days ago, run every week".** That is
  what access time is for: Python reads `pyvenv.cfg` on every interpreter start,
  node reads each package's `package.json` when it loads it. Measured: running
  a venv's python updated the atime of `pyvenv.cfg` immediately. On Linux with
  the default `relatime` mount, atime updates at most once a day — enough for a
  threshold in days. On a `noatime` mount the signal is absent. **macOS (APFS)
  behaves differently and was never tested** — verify before relying on it.
- **Directory atime is polluted by the tool itself**: listing a directory (as
  `du` does) updates its atime. Only file atimes are usable, and the tool must
  only `stat` the marker files, never read them.
- **Editors scan venvs by running them.** An IDE's interpreter discovery
  executed the python of 23 venvs within one minute — one of them untouched for
  270 days. At file level this is indistinguishable from real use (it even
  reads the `.pth` files). What gives it away is the shape: real use comes one
  artifact at a time, a scan comes in a burst. Rule that worked: a read shared
  by 5+ artifacts within 5 minutes is a scan and is ignored.
- **A directory outside git is never stale.** There is no activity signal, and
  a guess there deletes someone's only copy.
- **Docker records no last-use time for an image.** `CreatedAt` is the *build*
  time — for a pulled image, upstream's (a `postgres:16` pulled yesterday is
  "built 8 weeks ago"). `LastTagTime` stays zero after a pull.
- **Docker's event history is useless for this**: the daemon keeps a small
  in-memory buffer (~256 events) and container healthchecks fill it within
  minutes. Measured: zero `start` events retrievable for the previous 7 days.

### The usage history (the fix for the above)

Every signal above only shows the latest moment: atime keeps one timestamp (a
scan erases the real use before it) and Docker keeps none. The fix was a
**persistent usage history**: every run notes what it sees in use (an image
with a container, an artifact read outside a scan burst), and staleness is
computed from the newest of today's signals and the history.

Rules that made it safe:

- **It only ever adds evidence of use.** Effective last use is
  max(today's signals, recorded). A lost or corrupt history file then brings
  back exactly the stateless behaviour — never one extra deletion. That
  property was tested directly.
- **Observe often, clean rarely.** Docker containers live minutes, so the
  observation runs hourly (cheap: ~3 s); file atimes have day resolution, so
  file trees are walked about once a day. One self-pacing scheduled job beats
  two.
- **Merge on save, under a lock.** The scheduled observer and a manual run
  overlap; neither may lose the other's notes (keep max "used", min "first").
  Write atomically (temp file + rename).
- **Prune only what was actually covered.** This was a review finding, verified:
  pruning "entries not seen this run" for a whole category wiped the history
  when the Docker daemon was down, when a different scan root was passed, or
  when an unreadable subdirectory was skipped. Not seeing a thing is not
  evidence that it is gone. Each collector must report what it covered; prune
  only inside that, and never under paths it could not read.
- **Tests must never touch the real history file.** A prototype test wrote the
  real state file and marked every category as "just observed", which would
  have skipped the next real observation for a day.

## Deleting safely

- **Re-validate at the moment of deletion, with the same rule that classified
  it** — not just "is it under the expected dir". A review found that a
  location-only guard would let any classification bug delete every venv. The
  re-check also handles the project reappearing between report and delete.
- **Identify artifacts by name AND a marker**: `node_modules` needs a
  `package.json` beside it, `.venv` needs `pyvenv.cfg` inside, a Rust `target`
  needs `Cargo.toml` beside it. A Maven `target` is not ours to judge.
- **Never follow symlinks**, never descend into an artifact (nested
  `node_modules` are part of the outer one).
- **"Missing marker" is not evidence.** A venv without its project-pointer file
  may be one still being created, or one made by another tool. Report it,
  never delete it. (Review finding, verified.)
- **Read-only trees**: Go's module cache and some `node_modules` contain
  read-only directories. Unlinking needs write on the *parent* dir. When
  granting it: add bits (never replace the mode), only inside the tree being
  deleted (never the directory that holds it), only on a permission error, and
  re-raise the original error if the retry fails. A test caught the first
  version failing; a review caught the second clobbering the parent's mode.
- **Image removal without force**: an image something still depends on stays,
  and the error says why.
- **Volumes are never automatic.** "No container uses it" is exactly the state
  of a database volume between `compose down` and `up`.
- **Report the space actually freed** by measuring the filesystem before and
  after. Per-item sizes overstate: image layers are shared, and uv hard-links
  its cache into venvs.

## Package-manager specifics that bit

- **pipenv** records the project path in `<venv>/.project`; the venv name is
  derived from that path, so a venv for a vanished path can never be reused —
  safe to call garbage.
- **Version-manager shims (mise, asdf)** may not resolve outside a project
  directory or under a service manager: `npm` failed with "no version is set
  for shim". Caches should be located without invoking the tool when the
  location is known (npm's `_cacache`), and a tool that is installed but fails
  must be reported as skipped, not silently dropped from the report.
- **A failing tool may print nothing.** Don't assume there is an error line.
- **yarn berry's global cache is read live** by Plug'n'Play projects — deleting
  it breaks them until reinstall. Manual tier.
- **Docker's `builder prune`** (buildx) already removes all unused cache without
  `--all`; `buildx du` reports much more but as *Shared* — layers images still
  hold, which no prune frees.
- **Disk percentage**: compute it like `df` (used / (used + available)); the
  root-reserved blocks otherwise skew it by several points.

## Running unattended

- A **service manager runs with a minimal PATH**: version-manager shims and
  user bin dirs are missing. The prototype had to set PATH explicitly in the
  unit. A Go binary avoids most of this, but still shells out to `docker`,
  `npm`, `uv`, `go`…
- **Notifications are a plugin, not a dependency.** The prototype hard-wired a
  push-notification helper. One bug: the failure report started with `- `,
  which the helper parsed as an unknown option, so **the failure alert — the
  one case "quiet" mode promises to send — never arrived**. Pass a `--`
  separator or send the body on stdin. Also: some helpers exit 0 even when the
  server refused the message; check the actual result.
- **Quiet mode**: notify only above the disk threshold or when a step failed.
- **Trial mode first**: the scheduled job shipped as report-only, and was only
  switched to unattended cleaning after the report had been checked by hand.

## Testing approach that paid off

- Pure functions for every decision (classification, tier selection, scan
  detection, history merge), tested without Docker or a network.
- **Mutation-check every safety test**: deliberately reintroduce the bug and
  confirm the test fails. Every safety property in the prototype was verified
  this way; one test that "passed" would otherwise have proven nothing.
- Fake external commands (a fake `curl`, a fake notifier) instead of mocking
  the code under test.

## What the prototype got wrong for a public tool

These were fine for one machine and must change:

- Scan roots, the "big files" watch list and thresholds were constants.
- Linux-only assumptions: `du -x`, systemd timers, snap, journald,
  `/var/lib/snapd`. macOS needs its own equivalents (launchd, Homebrew cache,
  Xcode DerivedData, simulators, `~/Library/Caches`) — none of it was explored.
- It assumed a specific notifier and topic.
- Docker was assumed to be Docker Engine on the same filesystem as the home
  directory (true on that machine, measured; not true for Docker Desktop on
  macOS, where images live inside a VM disk).
