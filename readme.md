# Rootnet CLI 🚀

A Go CLI/TUI for managing and connecting to project servers. It replaces messy Bash aliases with a single binary featuring a fuzzy-search TUI and partial-match resolution.

## Features
* **Fuzzy Search:** Built-in TUI (via Charm Bubble Tea v2) for picking a host.
* **Partial Matching:** `rootnet get foo` resolves partial names instantly if a unique match is found.
* **Script friendly:** the picker renders on stderr, so `ssh $(rootnet get foo)` only ever captures `user@host`.
* **Portable:** Compiles to a single binary.

See [docs/PLAN.md](docs/PLAN.md) for the v2 roadmap.

---

## Installation

### 1. Prerequisites
* **Go 1.26+** (older Go toolchains will download 1.26 automatically)
* [Optional but recommended] **Fira Code** or any Nerd Font for the best TUI experience.

### 2. Build & Install
```bash
go install github.com/ProductionPanic/rootnet-cli/cmd/rootnet@latest
```

This installs a binary called `rootnet`.
