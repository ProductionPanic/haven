# Rootnet CLI 🚀

A Go CLI/TUI for managing and connecting to project servers. It replaces messy Bash aliases with a single binary featuring a fuzzy-search TUI and partial-match resolution.

## Features
* **Partial matching:** `rootnet foo` connects instantly when exactly one host matches, and opens a fuzzy picker otherwise.
* **Frecency ranking:** hosts you use often and recently float to the top.
* **Rich host records:** port, identity file, jump host, start directory, extra ssh args, environment, tags and notes.
* **Script friendly:** the picker renders on stderr, so `ssh $(rootnet get foo)` only ever captures `user@host`.
* **Shell completion** of host names, tags and flags.
* **Portable:** a single static binary (SQLite via pure Go, no cgo).

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

---

## Usage

```
rootnet [query]              connect (unique match → ssh, otherwise a picker)
rootnet ssh <query>          explicit connect (for hosts named like a subcommand)
rootnet get [query]          print user@host  (--format '{{.User}}@{{.Hostname}}:{{.Port}}', --json)
rootnet ls                   list hosts (--tag, --recent, --json)
rootnet add [name] [user@host] [flags]   add a host; without a destination an interactive form opens
rootnet edit <query> [flags] edit a host; without field flags an interactive form opens
rootnet rm <query>           remove a host (asks for confirmation unless --yes)
rootnet import [file]        import TOML or the legacy "name | user@host" format
rootnet export               export as TOML or ssh_config (--format ssh-config -o ~/.ssh/config.d/rootnet)
rootnet completion zsh|bash|fish
```

When a host has a remote path, `rootnet` logs in and `cd`s there:
`ssh -t user@host 'cd /var/www/site && exec "$SHELL" -l'`.

### Storage

Hosts live in a SQLite database at `$XDG_CONFIG_HOME/rootnet/rootnet.db`
(default `~/.config/rootnet/rootnet.db`). Override it with `ROOTNET_DB` or `--db`.

On first run, an existing `~/rootnet_hosts.txt` is imported automatically and
renamed to `~/rootnet_hosts.txt.bak`. Use `rootnet export > hosts.toml` /
`rootnet import hosts.toml` for bulk edits, backups and syncing between machines.

### Shell completion

```bash
# zsh
rootnet completion zsh > "${fpath[1]}/_rootnet"
# bash
rootnet completion bash > /etc/bash_completion.d/rootnet
# fish
rootnet completion fish > ~/.config/fish/completions/rootnet.fish
```
