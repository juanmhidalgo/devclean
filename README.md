# devclean

Reclaim the disk that development work leaves behind — stale Docker images,
`node_modules` and virtualenvs of projects you no longer use, orphaned
environments and package caches — sorted by how much it costs to be wrong.

A single Go binary for Linux and macOS. **Status: specification.** Nothing is
implemented yet.

- [`docs/lessons-from-prototype.md`](docs/lessons-from-prototype.md) — what a
  working prototype taught: what fills a disk, how to tell "unused" apart from
  "used rarely", and the deletion bugs reviews caught.
