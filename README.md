# devclean

Reclaim the disk that development work leaves behind — stale Docker images,
`node_modules` and virtualenvs of projects you no longer use, orphaned
environments and package caches — sorted by how much it costs to be wrong.

A single Go binary for Linux; macOS is unsupported.

**Status: early.** Everything in v1 is implemented and runs on the author's
machine in trial mode (scheduled reports, garbage-only cleaning). Deleting
stale items has not been exercised in real use yet; until it has, run
`clean --dry-run` first and keep the schedule in trial mode.

## Install

```
go install github.com/juanmhidalgo/devclean/cmd/devclean@latest
devclean init        # write a config from what it finds; offers the trial schedule
devclean report      # see what could be reclaimed; deletes nothing
```

Requires Go 1.27 or newer. `docker`, `uv`, `pre-commit` and `snap` are used
when present and skipped otherwise.

## Usage

```
devclean                          # show this help
devclean init [--yes] [--dry-run] [--force] [--notify-command CMD]
devclean report [--summary] [--notify [--quiet]]  # show what could be reclaimed; deletes nothing
devclean clean [--dry-run] [--yes] [--quiet]
devclean observe                  # record usage observations; deletes nothing
devclean schedule install [--report-only]
devclean schedule uninstall
```

- `init` writes a first config: it finds the home directories holding git
  repositories (scan roots) and large disk images (watch list), asks which to
  keep and for a notification command (sending a test one), then offers to
  schedule devclean in trial mode and to record a first observation. `--yes`
  takes everything found without asking; `--dry-run` prints the config and
  writes nothing; an existing config is replaced only with `--force` (kept as
  `config.toml.bak`).
- `report --summary` shows only the totals per tier and per category, without
  the item list (not combinable with `--json`). `report --notify` sends that
  summary, headed by the disk usage, to `notify_command`; with `--quiet` only
  when a filesystem is above the pressure threshold or a collector was skipped.
- `clean` deletes garbage and pressure-driven caches; stale items are deleted
  only when chosen (or with `--yes`). `--dry-run` prints what it would delete
  and records nothing; `--quiet` notifies only when something needs attention.
- Global flags: `--only` (categories: projects, docker, venvs, caches, system,
  watch), `--tier` (garbage, caches, stale, manual), `--root` (scan this root
  instead of the configured ones), `--json` (one JSON document) and `--color`
  (`auto`, `always` or `never`; `auto` colors only a terminal and honors
  `NO_COLOR`). `--only`, `--tier` and `--root` are repeatable.
- The report groups items that share a reason, lists the largest first and
  shows manual items as the command to run; a Docker volume that containers
  still mount shows those containers instead (`used_by` in JSON), since docker
  refuses to remove it. A closing Tips section says how to keep space from
  growing back (journald `SystemMaxUse`, snap `refresh.retain`, anonymous
  Docker volumes); `tip` in JSON. Sizes devclean cannot measure (a
  tool-native prune, a volume docker does not size) show as `-`, and as
  `"size_unknown": true` in JSON.
- `schedule install` schedules `observe` hourly and `clean` on the configured
  cadence; `--report-only` schedules `report` instead (trial mode), with
  `--notify` when `notify_command` is configured.
- Config lives at `$XDG_CONFIG_HOME/devclean/config.toml`.
- Exit codes: `0` ok, `1` fatal error, `2` partial run (a skipped collector, a
  failed deletion or a failed notification).

## Safety checks

Each safety test was shown to fail when the bug it guards against is
reintroduced; see [`docs/mutation-checks.md`](docs/mutation-checks.md).

## Decisions already made

- **Linux first.** macOS is out of scope for the first version; keep the design
  from closing the door on it (no Linux paths hard-coded outside a platform
  layer), but do not specify or build macOS support yet.
- **The name stays `devclean`**, whether or not it is taken elsewhere. The tool
  is published publicly but built for the author's own use first.

## Background

- [`docs/lessons-from-prototype.md`](docs/lessons-from-prototype.md) — what a
  working prototype taught: what fills a disk, how to tell "unused" apart from
  "used rarely", and the deletion bugs reviews caught.

## License

MIT — see [`LICENSE`](LICENSE).
