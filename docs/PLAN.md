# Rootnet v2 — Plan

> **Note:** rootnet has since been renamed to **haven**. This plan is kept
> as written; "rootnet" below refers to the same tool.

This document describes how to grow `rootnet` from a single-file host picker into a
full SSH companion: credential management, fast search/connect, and a two-pane
file manager. It's split into phases that can each be merged and used on their own.

---

## 1. Where we are today

- `main.go` (~170 lines): reads `~/rootnet_hosts.txt` (`name | user@host`, currently 88
  entries), fuzzy-matches the argument, opens a Bubbles `list` if the match is ambiguous,
  then `exec`s `ssh user@host` or prints `user@host` (`rootnet get`).
- Charm v1: `bubbletea v1.3.10`, `bubbles v0.21.0`, `lipgloss v1.1.0`.
- `atotto/clipboard` is still in `go.mod` even though clipboard support was removed.

Things worth fixing along the way:

1. **`rootnet get` with an ambiguous match writes the TUI to stdout.** The comment in
   `runFilter` says Bubble Tea renders to stderr, but v1 renders to **stdout** by default.
   So `ssh $(rootnet get foo)` captures escape codes when the picker opens. Fix: render the
   TUI to stderr (or `/dev/tty`) and print only the result to stdout.
2. **The installed binary is called `rootnet-cli`, not `rootnet`.** `go install` names the
   binary after the last element of the module path. Moving `main` to `cmd/rootnet/` makes
   `go install github.com/ProductionPanic/rootnet-cli/cmd/rootnet@latest` produce `rootnet`.
3. Subcommand names (`get`) collide with host names. Once there are more subcommands
   (`add`, `edit`, `files`...), reserve them and keep an explicit `rootnet ssh <name>` as the
   escape hatch.

---

## 2. Charm v2 migration

The v2 releases moved to vanity import paths and changed a lot of APIs. Current versions:

| Package    | v1 (now)                              | v2 (target)                         |
|------------|---------------------------------------|-------------------------------------|
| Bubble Tea | `github.com/charmbracelet/bubbletea`  | `charm.land/bubbletea/v2` (v2.0.x)  |
| Bubbles    | `github.com/charmbracelet/bubbles`    | `charm.land/bubbles/v2` (v2.2.x)    |
| Lip Gloss  | `github.com/charmbracelet/lipgloss`   | `charm.land/lipgloss/v2` (v2.0.x)   |
| Huh (forms)| —                                     | `charm.land/huh/v2`                 |

All three core packages have to be upgraded together (Bubbles v2 requires the other two).
Watch out: `github.com/charmbracelet/bubbletea/v2` is **not** a valid path; it has to be
`charm.land/...`.

Breaking changes that affect this code:

- `View() string` becomes `View() tea.View`. Program options like `tea.WithAltScreen()`
  become fields on the view (`v.AltScreen = true`). The same goes for mouse mode, window
  title and cursor.
- `tea.KeyMsg` becomes `tea.KeyPressMsg`; `msg.Type/Runes/Alt` become `msg.Code/Text/Mod`;
  `" "` becomes `"space"`.
- Mouse messages are split up (`MouseClickMsg`, `MouseWheelMsg`, ...) and the button
  constants are renamed.
- Bubbles components use getters/setters (`SetWidth`, `SetHeight`...) instead of exported
  fields in many places. Styles are now set explicitly rather than detected implicitly.
- Lip Gloss v2 has no global renderer. Colors are `color.Color`, and light/dark adaptation
  happens through the background color reported by Bubble Tea (`tea.BackgroundColorMsg`),
  not through automatic detection.

New features worth using:

- The new "Cursed" renderer is faster and diff-based, which matters for a dual-pane file
  manager that redraws often.
- Better keyboard support (disambiguated keys, key release events), so bindings like
  `ctrl+enter`, `shift+tab` and `ctrl+h` vs `backspace` work reliably in modern terminals.
- Built-in OSC52 clipboard (`tea.SetClipboard`), so "copy user@host" can come back without
  the external dependency, and it also works over SSH.
- Synchronized output and better background-color detection for theming.

**Approach:** do the migration first, as its own small PR, on the existing 170 lines.
It's the cheapest way to learn the new API before building on it.

Refs: [Bubble Tea upgrade guide](https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md),
[Bubbles upgrade guide](https://github.com/charmbracelet/bubbles/blob/main/UPGRADE_GUIDE_V2.md),
[What's new in v2](https://github.com/charmbracelet/bubbletea/discussions/1374).

---

## 3. Storage: move to SQLite

**Recommendation: yes, SQLite**, using `modernc.org/sqlite` (pure Go, no cgo). It keeps
the "single static binary" property and `go install` keeps working everywhere.

Why not just a better text file (TOML/YAML)?

- We want more than `name → user@host`: port, key, jump host, default remote path, tags,
  notes, plus usage stats (last used, use count) for ranking. Usage stats get written on
  every connect, which is awkward in a hand-edited file.
- Edits come from the TUI, so atomic writes and no partial-file corruption matter.
- Room to grow: transfer bookmarks, per-host history, saved tunnels.

What we lose is "open it in vim and edit." That's covered by `rootnet export` / `rootnet import`
(TOML) for bulk edits, backups and dotfile syncing.

**Location:** `$XDG_CONFIG_HOME/rootnet/rootnet.db`, falling back to `~/.config/rootnet/rootnet.db`.
Avoid `os.UserConfigDir()`, which gives `~/Library/Application Support` on macOS and is
annoying for dotfiles. It can be overridden with `ROOTNET_DB` or `--db`.

**Migration:** on first run, if the DB doesn't exist and `~/rootnet_hosts.txt` does, import it
automatically, rename the old file to `rootnet_hosts.txt.bak`, and print a one-line notice.
`user@host` gets split into `user` + `hostname`.

### Schema (v1)

```sql
CREATE TABLE hosts (
    id            INTEGER PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,      -- "appelenburg.nl"
    user          TEXT NOT NULL DEFAULT '',
    hostname      TEXT NOT NULL,             -- "monotone-nuthatch.sys.rootnet.io" or an ~/.ssh/config alias
    port          INTEGER NOT NULL DEFAULT 22,
    identity_file TEXT NOT NULL DEFAULT '',
    jump_host     TEXT NOT NULL DEFAULT '',  -- ProxyJump
    remote_path   TEXT NOT NULL DEFAULT '',  -- e.g. /var/www/site; used for `cd` on login and as the file manager start dir
    extra_args    TEXT NOT NULL DEFAULT '',  -- raw extra ssh options
    notes         TEXT NOT NULL DEFAULT '',
    environment   TEXT NOT NULL DEFAULT '',  -- production / staging / dev → colored badge, confirm on destructive ops
    last_used_at  DATETIME,
    use_count     INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE tags      (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE);
CREATE TABLE host_tags (host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE,
                        tag_id  INTEGER REFERENCES tags(id)  ON DELETE CASCADE,
                        PRIMARY KEY (host_id, tag_id));
CREATE TABLE bookmarks (id INTEGER PRIMARY KEY, host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE,
                        side TEXT NOT NULL CHECK (side IN ('local','remote')), path TEXT NOT NULL);
```

Migrations are plain numbered SQL files embedded with `embed`, tracked through
`PRAGMA user_version`. No migration framework needed.

**Secrets:** don't store passwords or private keys in the DB. Rely on keys and ssh-agent,
as now. If password hosts ever come up, use the macOS Keychain via `zalando/go-keyring`,
keyed by host id. That's out of scope for now.

---

## 4. CLI surface

Use **Cobra**, optionally wrapped in **Fang** (`charm.land/fang/v2`) for styled help and
errors. The main reason to use Cobra is **shell completion of host names**:
`rootnet appe<TAB>` → `rootnet appelenburg.nl`.

```
rootnet [query]              connect (unchanged behaviour: unique match → ssh, else picker)
rootnet ssh <query>          explicit connect (works even if a host is named like a subcommand)
rootnet get [query]          print user@host (unchanged; TUI on stderr)
    --format '{{.User}}@{{.Hostname}}:{{.Port}}'   / --json
rootnet ui                   full TUI (host manager); also opened by `rootnet` with no args
rootnet ls [--tag x] [--json]
rootnet add [name] [user@host] [flags]        no args → interactive Huh form
rootnet edit <query>                          Huh form, prefilled
rootnet rm <query>                            with confirmation
rootnet files [query]        two-pane file manager
rootnet cp <src> <dst>       quick scp-like copy: `rootnet cp ./dump.sql appelenburg:/tmp/`
rootnet import [file] / export [--format toml|ssh-config]
rootnet completion zsh|bash|fish
```

Matching stays the same, but results are **ranked by frecency** (use count weighted by how
recently a host was used), so the sites you touch most float to the top of the picker.
An exact name match always wins over partial matches.

**Connecting** keeps using the system `ssh` binary through `exec`, so `~/.ssh/config`, the
agent, ControlMaster and so on all keep working. We build the args from the stored fields
(`-p`, `-i`, `-J`, extra args), and if `remote_path` is set we run
`ssh -t host 'cd <path> && exec $SHELL -l'`. On Unix it's worth using `syscall.Exec` so
`rootnet` replaces itself with `ssh`: cleaner signals, and no lingering parent process.

---

## 5. TUI architecture

One root model that routes between screens, each screen being its own `tea.Model`-like
component:

```
App (root)
├── HostsScreen      list + filter, detail side panel, actions
├── HostForm         huh v2 form (add/edit), opened as an overlay
├── ConfirmDialog    generic yes/no overlay
└── FilesScreen      two panes + transfer queue
```

- Screens talk to the root through messages (`openFilesMsg{host}`, `connectMsg{host}`,
  `hostSavedMsg`...), not by reaching into each other.
- The root owns the window size, theme and global keys (`ctrl+c`, `?` for help).
- Keybindings are defined per screen with `bubbles/key` and rendered with `bubbles/help`.
- A single `theme` package holds Lip Gloss styles, built from the `BackgroundColorMsg`
  (light/dark).
- Connecting to SSH from inside the TUI: quit the program with a "connect" result and `exec`
  ssh afterwards, as now. For "open shell and come back to the TUI afterwards," use
  `tea.ExecProcess`.

### Hosts screen

- Fuzzy filter as you type (Bubbles list filter, or a custom `textinput` + `sahilm/fuzzy`
  for full control over ranking).
- Each row shows the name, `user@host`, an environment badge (red for prod) and tags.
- Grouping: many sites share one server (`monotone-nuthatch...`), so a "group by server"
  toggle is useful.
- Detail panel on the right for the selected host: all fields, notes, last used.
- Keys: `enter` ssh · `f` files · `a` add · `e` edit · `d` delete · `y` copy user@host (OSC52)
  · `t` filter by tag · `g` group by server · `/` filter.

---

## 6. File manager (`rootnet files`)

A FileZilla/Midnight Commander-style manager: local on the left, remote on the right,
transfer queue at the bottom.

```
┌ Local: ~/Projects/appelenburg ─────────┐┌ appelenburg.nl: /var/www/site ─────────┐
│ ..                                     ││ ..                                     │
│ ▸ wp-content/              —   12 Mar  ││ ▸ wp-content/              —   01 Oct  │
│ * dump.sql             48.2M   02 Oct  ││   wp-config.php          3.1K   14 Feb  │
│   .env                  512B   20 Feb  ││   .htaccess              1.2K   14 Feb  │
└────────────────────────────────────────┘└────────────────────────────────────────┘
 ↑ dump.sql → /var/www/site   ██████████░░░░░░  62%  12.4 MB/s   ETA 3s
 tab switch · space mark · c copy → · m mkdir · r rename · d delete · . hidden · q quit
```

### Building blocks

**FS abstraction.** Both panes work against one interface, so the pane code is identical
for both sides and easy to test:

```go
type FS interface {
    ReadDir(path string) ([]fs.FileInfo, error)
    Stat(path string) (fs.FileInfo, error)
    Open(path string) (io.ReadCloser, error)
    Create(path string) (io.WriteCloser, error)
    Mkdir(path string) error
    Remove(path string) error   // recursive for dirs
    Rename(from, to string) error
    Chtimes / Chmod(...)        // to preserve mtime/permissions
    Join, Dir, Home(...)        // path semantics differ (remote is always POSIX)
}
```

`LocalFS` wraps `os`; `RemoteFS` wraps `*sftp.Client` (`github.com/pkg/sftp`).

**How to get an SFTP connection.** Recommended: run the system ssh binary as the transport.

```go
cmd := exec.Command("ssh", append(sshArgs(host), "-o", "BatchMode=yes", "-s", "sftp")...)
// wire cmd.Stdin/Stdout pipes → sftp.NewClientPipe(stdout, stdin)
```

This reuses everything the shell connection already uses: `~/.ssh/config`, agent,
ProxyJump, ControlMaster, known_hosts. It behaves exactly like `rootnet [name]`, with no need
to reimplement SSH config parsing or auth. `BatchMode=yes` keeps ssh from prompting inside
the TUI. If a host needs interactive auth, we show a clear error suggesting a key or agent.

The alternative is native `golang.org/x/crypto/ssh` + `knownhosts` + agent. It gives more
control, but we'd have to re-implement `~/.ssh/config` handling. Keep it as a fallback idea.

**Transfers.** A `transfer` package with a job queue:

- A job is a direction, a source FS and path, and a destination FS and path. Directories are
  walked recursively and expanded into file jobs, with the total size computed up front for
  overall progress.
- 2–4 worker goroutines. Progress is reported through an `io.Writer` counter that sends
  throttled (~10/s) `transferProgressMsg` to the program. `bubbles/progress` renders it.
- Use `sftp` concurrent reads and writes (`UseConcurrentWrites`, `ReadFrom`/`WriteTo`) for
  throughput. Large files over a single SFTP channel are otherwise slow.
- Conflicts: a prompt with overwrite / skip / overwrite if newer / rename, plus "apply to all".
- Cancellation via `context`. Partial files are written as `name.rootnet-part` and renamed
  on completion, so a cancelled upload never leaves a half-written `wp-config.php` in place.
- Preserve mtime (and mode on upload, optionally).
- Production hosts: confirm before delete or overwrite on the remote side.

**Pane features:** sorting (name/size/mtime), hidden-files toggle, filter-as-you-type,
symlink display, multi-select, bookmarks (stored in the DB), starting in the host's
`remote_path`, and remembering the last local dir per host.

**Stretch:** `e` on a remote file downloads it to a temp dir, opens `$EDITOR` via
`tea.ExecProcess`, and uploads it back if it changed (with a check that the remote file
didn't change in the meantime).

---

## 7. Proposed package layout

```
cmd/rootnet/main.go          tiny: build root command, run
internal/cli/                cobra commands (root, get, add, edit, rm, ls, files, cp, import, export)
internal/store/              sqlite open, migrations (embed), Host model, CRUD, frecency query
internal/store/migrations/   001_init.sql, ...
internal/legacy/             import of ~/rootnet_hosts.txt
internal/sshx/               ssh arg building, exec/connect, sftp-over-ssh client
internal/vfs/                FS interface, LocalFS, RemoteFS
internal/transfer/           queue, workers, progress, conflict policy
internal/tui/                app root + router, theme, keys
internal/tui/hosts/          hosts screen
internal/tui/hostform/       huh forms
internal/tui/files/          file manager screen + pane component
```

---

## 8. Phases

Each phase ends in a working, releasable state.

**Phase 0: housekeeping (small)**
- Move main to `cmd/rootnet`, fix the binary name and README install instructions.
- Fix `get` rendering the TUI on stdout.
- Drop the unused clipboard dependency.

**Phase 1: Charm v2 migration (small)**
- Port the existing picker to `charm.land/bubbletea/v2`, `bubbles/v2`, `lipgloss/v2`.
- No new features. Confirms the toolchain and the new API.

**Phase 2: storage + CLI (medium)**
- `internal/store` with SQLite, migrations, and auto-import of the txt file.
- Cobra commands: `ls`, `add`, `edit`, `rm`, `get --format/--json`, `ssh`, `import`/`export`,
  and completion with host names.
- Frecency ranking, plus the ssh arg builder (port/key/jump/remote_path).
- Tests: store against a temp DB, matcher/ranking, legacy import, ssh arg builder.

**Phase 3: host manager TUI (medium)**
- Root app + router, hosts screen with detail panel, Huh add/edit form, delete confirm,
  tags, environment badges, group by server, copy to clipboard.
- Tests: `teatest` golden snapshots for the main screens.

**Phase 4: file manager (large)**
- 4a: `vfs` + sftp-over-ssh + single-pane browsing of remote.
- 4b: dual pane, navigation, mkdir/rename/delete.
- 4c: transfer queue with progress, recursive dirs, conflicts, cancellation.
- 4d: `rootnet cp` built on the same transfer engine.
- Tests: `RemoteFS` against an in-process `sftp.NewRequestServer` over `net.Pipe`. That
  needs no real server and works in CI.

**Phase 5: extras (pick and choose)**
- See below.

---

## 9. Extra ideas

Rough order of value versus effort:

1. **Export to `~/.ssh/config`** (`rootnet export --format ssh-config > ~/.ssh/config.d/rootnet`).
   Every host then works with `scp`, `rsync`, VS Code Remote and PhpStorm deployment too.
   Also the reverse: import from `~/.ssh/config`.
2. **Connection health check.** In the hosts screen, press `p` to ping all hosts
   concurrently (TCP dial to port 22 with a timeout) and show a green/red dot. Useful for
   spotting dead sites quickly.
3. **Run a command on one or many hosts.** `rootnet run --tag wordpress 'wp core version'`,
   with output collected per host. Great for checking versions across all sites on one server.
4. **Tunnel presets.** Save `-L 3307:localhost:3306` style forwards per host
   (`rootnet tunnel appelenburg db`), for opening a DB in TablePlus or DataGrip.
5. **Notes per host**, rendered as markdown with Glamour (`charm.land/glamour/v2`), for
   things like "DB creds in 1Password under X" or "deploy via git pull in /var/www".
6. **Quick-edit remote files** in `$EDITOR` (see the file manager stretch goal).
7. **Activity log**: when you connected where, and what was transferred. This is cheap with
   SQLite already there.
8. **Database dump helper**: `rootnet dump appelenburg` runs `mysqldump`/`wp db export`
   remotely and downloads the result through the transfer engine.

---

## 10. Open questions

1. Is this used on more than one machine? If so, should the DB live somewhere synced, or is
   `export`/`import` (TOML in dotfiles) enough?
2. Do any hosts need password or 2FA auth, or is everything key/agent-based? This decides
   whether `BatchMode=yes` for SFTP is acceptable.
3. Are jump hosts or non-22 ports needed today, or is `user@host` always enough?
4. Should `rootnet` with no arguments open the full host manager, or keep opening the simple
   picker (and use `rootnet ui` for the manager)?
5. Which extras from section 9 are most interesting, if any?
