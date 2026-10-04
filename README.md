# devclean

Reclaim the disk that development work leaves behind — stale Docker images,
`node_modules` and virtualenvs of projects you no longer use, orphaned
environments and package caches — sorted by how much it costs to be wrong.

A single Go binary. **Status: specification.** Nothing is implemented yet.

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
