# devclean

Reclaim the disk that development work leaves behind — stale Docker images,
`node_modules` and virtualenvs of projects you no longer use, orphaned
environments and package caches — sorted by how much it costs to be wrong.

A single Go binary. **Status: v1 implemented on Linux; macOS is unsupported.**

## Usage

```
devclean                          # show this help
devclean report [--summary]       # show what could be reclaimed; deletes nothing
devclean clean [--dry-run] [--yes] [--quiet]
devclean observe                  # record usage observations; deletes nothing
devclean schedule install [--report-only]
devclean schedule uninstall
```

- `report --summary` shows only the totals per tier and per category, without
  the item list (not combinable with `--json`).
- `clean` deletes garbage and pressure-driven caches; stale items are deleted
  only when chosen (or with `--yes`). `--dry-run` prints what it would delete
  and records nothing; `--quiet` notifies only when something needs attention.
- Global flags: `--only` (categories: projects, docker, venvs, caches, system,
  watch), `--tier` (garbage, caches, stale, manual), `--root` (scan this root
  instead of the configured ones) and `--json` (one JSON document). `--only`,
  `--tier` and `--root` are repeatable.
- `schedule install` schedules `observe` hourly and `clean` on the configured
  cadence; `--report-only` schedules `report` instead (trial mode).
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
