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
rootnet                      open the host manager (also: rootnet ui)
rootnet [query]              connect (unique match → ssh, otherwise a picker)
rootnet ssh <query>          explicit connect (for hosts named like a subcommand)
rootnet get [query]          print user@host  (--format '{{.User}}@{{.Hostname}}:{{.Port}}', --json)
rootnet ls                   list hosts (--tag, --recent, --json)
rootnet add [name] [user@host] [flags]   add a host; without a destination an interactive form opens
rootnet edit <query> [flags] edit a host; without field flags an interactive form opens
rootnet rm <query>           remove a host (asks for confirmation unless --yes)
rootnet files [query]        two-pane file manager (local ↔ host)
rootnet cp <src>... <dst>    copy files, e.g. rootnet cp ./dump.sql appel:/tmp/
rootnet import [file]        import TOML or the legacy "name | user@host" format
rootnet export               export as TOML or ssh_config (--format ssh-config -o ~/.ssh/config.d/rootnet)
rootnet completion zsh|bash|fish
```

When a host has a remote path, `rootnet` logs in and `cd`s there:
`ssh -t user@host 'cd /var/www/site && exec "$SHELL" -l'`.

### Host manager

`rootnet` without arguments opens a full-screen manager: a filterable host
list with a detail panel (all fields, notes, last use and the exact ssh
command).

| Key | Action |
|-----|--------|
| `enter` | ssh to the host (rootnet exits and becomes ssh) |
| `s` | ssh and return to the manager afterwards |
| `/` | filter (typing filters live, `enter` connects to the selection) |
| `esc` | leave the filter, press again to clear it |
| `t` | cycle through tags |
| `g` | group by server |
| `a` / `e` / `d` | add / edit / delete |
| `f` | open the file manager for the host (`q` comes back) |
| `y` | copy `user@host` to the clipboard (OSC 52, works over ssh too) |
| `?` | all key bindings |

Production hosts get a red badge and an extra warning before deletion.

### File manager

`rootnet files appel` (or `f` in the host manager) shows your local files on
the left and the host on the right. The local side starts in your current
directory; the remote side starts in the host's remote path, or where you left
off last time on that host.

| Key | Action |
|-----|--------|
| `tab` | switch pane |
| `enter` / `⌫` | open directory / go up |
| `space`, `*` | mark, mark all |
| `c` | copy marked (or selected) items to the other pane, directories recursively |
| `m` / `r` / `d` | mkdir / rename / delete (with confirmation) |
| `.` / `s` / `/` | hidden files / cycle sort / filter |
| `x` | cancel running transfers |

Files and folders get [Nerd Font](https://www.nerdfonts.com) icons by type
(PHP, JS, images, archives, `wp-config.php`, `wp-content/`, `Dockerfile`, …).
If your terminal font isn't a Nerd Font, turn them off with
`export ROOTNET_ICONS=off` to get plain markers instead.

When a file already exists you can overwrite, skip, overwrite if newer or keep
both, optionally for all remaining files. Production hosts get an extra warning.

`rootnet cp` uses the same engine from the command line, with `host:path` for
remote locations (relative remote paths start in the login directory):

```bash
rootnet cp ./dump.sql appel:/tmp/
rootnet cp appel:/var/www/site/wp-config.php .
rootnet cp --newer ./theme appel:wp-content/themes/
```

Transfers run over your normal `ssh` (same `~/.ssh/config`, keys and agent,
including 1Password's SSH agent) using the SFTP subsystem, with several files
in flight at once. Files are written as `name.rootnet-part` and only renamed
into place when complete, so a cancelled upload never leaves a half-written
file behind. Modification times and permissions are preserved.

The file manager connects without prompting in the terminal (`BatchMode`), so
hosts need key or agent authentication; if a host's key isn't known yet,
connect once with `rootnet ssh <host>` first.

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
