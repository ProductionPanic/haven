# Haven ⚓

A safe harbour for your SSH servers: one terminal tool to find and connect to
hosts, browse and edit their files, and copy files between machines. A single
Go binary with a fuzzy-search TUI, built on your normal `ssh`, so your
`~/.ssh/config`, keys and agent just work.

> Haven used to be called **rootnet**. See [Coming from rootnet](#coming-from-rootnet).

![haven host manager: browsing, filtering, grouping by server and tags](docs/demo.gif)

## Features
* **Partial matching:** `haven foo` connects instantly when exactly one host matches, and opens a fuzzy picker otherwise.
* **Frecency ranking:** hosts you use often and recently float to the top.
* **Rich host records:** port, identity file, jump host, start directory, extra ssh args, environment, tags and notes.
* **Script friendly:** the picker renders on stderr, so `ssh $(haven get foo)` only ever captures `user@host`.
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
go install github.com/ProductionPanic/haven/v2/cmd/haven@latest
```

This installs a binary called `haven`.

---

## Usage

```
haven                      open the host manager (also: haven ui)
haven [query]              connect (unique match → ssh, otherwise a picker)
haven ssh <query>          explicit connect (for hosts named like a subcommand)
haven get [query]          print user@host  (--format '{{.User}}@{{.Hostname}}:{{.Port}}', --json)
haven ls                   list hosts (--tag, --recent, --json)
haven add [name] [user@host] [flags]   add a host; without a destination an interactive form opens
haven add ssh <user@host>  add a host straight from an ssh destination, named after the user
haven edit <query> [flags] edit a host; without field flags an interactive form opens
haven rm <query>           remove a host (asks for confirmation unless --yes)
haven check [query]        check that ssh logins still work (--tag, --json; exit status 1 on failure)
haven files [query]        two-pane file manager (local ↔ host)
haven cp <src>... <dst>    copy files, e.g. haven cp ./dump.sql appel:/tmp/
haven import [file]        import TOML or the legacy "name | user@host" format
haven export               export as TOML or ssh_config (--format ssh-config -o ~/.ssh/config.d/haven)
haven completion zsh|bash|fish
```

When a host has a remote path, `haven` logs in and `cd`s there:
`ssh -t user@host 'cd /var/www/site && exec "$SHELL" -l'`.

### Checking connections

haven can tell you when a login stopped working (a revoked key, a removed
user, a server that is gone). A check is a real login without a terminal:
`ssh -o BatchMode=yes -o ConnectTimeout=5 user@host true`, with the host's
port, key, jump host and extra args. Up to 8 hosts are checked at once and the
last result is kept in the database.

- `haven check` checks every host (or the ones matching a query or `--tag`),
  prints a table and exits with status 1 when one fails, so it works from cron.
- `haven add` and `haven edit` try the connection after saving and warn when
  it fails. The host is saved either way; `--no-check` skips the check.
- The host manager checks every host that wasn't checked in the last 5 minutes
  in the background when it opens. The dot before each host shows the result:
  green when the login worked, red when it failed, grey when unknown or still
  running. The detail panel shows when it was checked, and `ls` has an SSH column.

### Host manager

`haven` without arguments opens a full-screen manager: a filterable host
list with a detail panel (all fields, notes, last use and the exact ssh
command).

| Key | Action |
|-----|--------|
| `enter` | ssh to the host (haven exits and becomes ssh) |
| `s` | ssh and return to the manager afterwards |
| `/` | filter (typing filters live, `enter` connects to the selection) |
| `esc` | leave the filter, press again to clear it |
| `t` | cycle through tags |
| `g` | group by server |
| `a` / `e` / `d` | add / edit / delete |
| `f` | open the file manager for the host (`q` comes back) |
| `y` | copy `user@host` to the clipboard (OSC 52, works over ssh too) |
| `p` / `P` | check the ssh login of the host / of every host |
| `?` | all key bindings |

Production hosts get a red badge and an extra warning before deletion.

### File manager

`haven files appel` (or `f` in the host manager) shows your local files on
the left and the host on the right. The local side starts in your current
directory; the remote side starts in the host's remote path, or where you left
off last time on that host.

![haven file manager: copying local files to a host and viewing a remote file](docs/files.gif)

| Key | Action |
|-----|--------|
| `tab` | switch pane |
| `enter` / `⌫` | open directory or view file / go up |
| `e` | edit the file in `$EDITOR` (vim, nvim, …) |
| `n` | new file (opens it in your editor) |
| `space`, `*` | mark, mark all |
| `c` | copy marked (or selected) items to the other pane, directories recursively |
| `m` / `r` / `d` | mkdir / rename / delete (with confirmation) |
| `.` / `s` / `/` | hidden files / cycle sort / filter |
| `x` | cancel running transfers |

`enter` on a file opens a read-only viewer with syntax highlighting, line
numbers and vim-style keys (`j/k`, `ctrl+d/u`, `g/G`, `/` search with `n/N`,
`w` wrap, `e` edit, `q` close). It reads at most the first 1 MB, so peeking
at a large log is instant.

`e` edits in your own editor (`$VISUAL`, `$EDITOR`, else nvim/vim/vi/nano).
Remote files are downloaded to a temporary copy and uploaded back only if you
changed them, atomically (no half-written file on the server). If the server
copy changed while you were editing, haven asks whether to overwrite it,
keep both, or not upload (your edit is kept locally). Production hosts ask
before uploading. Binary files and files over 20 MB ask or refuse first.

Files and folders get [Nerd Font](https://www.nerdfonts.com) icons by type
(PHP, JS, images, archives, `wp-config.php`, `wp-content/`, `Dockerfile`, …).
If your terminal font isn't a Nerd Font, turn them off with
`export HAVEN_ICONS=off` to get plain markers instead.

When a file already exists you can overwrite, skip, overwrite if newer or keep
both, optionally for all remaining files. Production hosts get an extra warning.

`haven cp` uses the same engine from the command line, with `host:path` for
remote locations (relative remote paths start in the login directory):

```bash
haven cp ./dump.sql appel:/tmp/
haven cp appel:/var/www/site/wp-config.php .
haven cp --newer ./theme appel:wp-content/themes/
```

Transfers run over your normal `ssh` (same `~/.ssh/config`, keys and agent,
including 1Password's SSH agent) using the SFTP subsystem, with several files
in flight at once. Files are written as `name.haven-part` and only renamed
into place when complete, so a cancelled upload never leaves a half-written
file behind. Modification times and permissions are preserved.

The file manager connects without prompting in the terminal (`BatchMode`), so
hosts need key or agent authentication; if a host's key isn't known yet,
connect once with `haven ssh <host>` first.

### Storage

Hosts live in a SQLite database at `$XDG_CONFIG_HOME/haven/haven.db`
(default `~/.config/haven/haven.db`). Override it with `HAVEN_DB` or `--db`.

On first run, an existing `~/rootnet_hosts.txt` is imported automatically and
renamed to `~/rootnet_hosts.txt.bak`. Use `haven export > hosts.toml` /
`haven import hosts.toml` for bulk edits, backups and syncing between machines.

### Coming from rootnet

Haven is the same tool under a new name. To switch:

```bash
go install github.com/ProductionPanic/haven/v2/cmd/haven@latest
rm "$(go env GOPATH)/bin/rootnet"          # the old binary
haven completion zsh > "${fpath[1]}/_haven" # regenerate completions
```

- Your hosts come along automatically: the first time haven runs, it moves
  `~/.config/rootnet/rootnet.db` to `~/.config/haven/haven.db` and says so.
- `ROOTNET_DB` and `ROOTNET_ICONS` still work; `HAVEN_DB` and `HAVEN_ICONS`
  take precedence.
- If you exported an ssh config, re-run `haven export --format ssh-config -o …`.
- Muscle memory: `alias rootnet=haven`.

### Shell completion

```bash
# zsh
haven completion zsh > "${fpath[1]}/_haven"
# bash
haven completion bash > /etc/bash_completion.d/haven
# fish
haven completion fish > ~/.config/fish/completions/haven.fish
```
