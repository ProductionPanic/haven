# Contributing to haven

Thanks for helping out. Bug reports and ideas go in
[GitHub issues](https://github.com/ProductionPanic/haven/issues); for anything
bigger than a small fix, open an issue first so we can agree on the approach
before you write the code.

## Building and testing

You need Go 1.26 or newer and an OpenSSH client.

```bash
git clone https://github.com/ProductionPanic/haven.git
cd haven
go build ./cmd/haven        # builds ./haven
go test ./...
go vet ./...
gofmt -l .                  # should print nothing
```

CI runs the same checks on Linux, macOS and Windows for every pull request.

To try a build without touching your own hosts, point it at a throwaway
database:

```bash
export HAVEN_DB=$(mktemp -d)/dev.db
sh docs/demo-hosts.sh       # optional: fill it with fictional hosts
./haven
```

### Tests

- Tests never use the network or a real server. Remote file systems are
  backed by an in-process SFTP server (`internal/vfs/vfstest`) that serves a
  temporary directory, and ssh login checks use a fake runner.
- That SFTP server only understands POSIX paths, so tests that use it are
  skipped on Windows. Run the full suite on macOS or Linux (or in WSL) when
  you change file transfer or remote file code.
- TUI screens are tested by feeding key messages to the Bubble Tea models
  and checking the rendered view.
- Use fictional names in tests, examples and docs (`acme-shop.example`,
  `web1.example.com`), never real client hosts.

## Code layout

```
cmd/haven/           main package
internal/cli/        cobra commands; one file per command group
internal/store/      SQLite database (hosts, tags, per-host state) and migrations
internal/match/      query matching and frecency ranking
internal/sshx/       building ssh arguments and running ssh / SFTP
internal/check/      "does the ssh login still work" checks
internal/vfs/        one file system interface for the local disk and SFTP
internal/transfer/   the copy engine behind the file manager and `haven cp`
internal/exchange/   TOML and ssh_config import/export
internal/hostlist/   the plain-text "name | user@host" import format
internal/tui/        Bubble Tea screens: hosts, files, hostform, picker, theme
```

The UI is built on [Charm](https://charm.land) v2 (Bubble Tea, Bubbles,
Lip Gloss, Huh). SQLite comes from `modernc.org/sqlite`, which is pure Go, so
haven builds without cgo. Keep it that way.

### Database migrations

Schema changes go in a new numbered file in `internal/store/migrations/`
(`004_something.sql`). Migrations run automatically when the database is
opened. Never edit a migration that has been released; add a new one.

### Adding a subcommand

Add the command in `internal/cli/` and register it in `rootCmd`. Also add its
name to `reserved()`, so a host can't be given a name that collides with it
(`haven <name>` connects to hosts). Update the usage list in the README, and
`internal/cli/llm.md` if the command is meant for scripts or LLMs.

## Pull requests

- Keep a pull request to one change, with tests.
- Run `go test ./...`, `go vet ./...` and `gofmt` before pushing.
- Update the README when you change behaviour users can see.
- The demo GIFs are recorded with [VHS](https://github.com/charmbracelet/vhs):
  see the comments at the top of `docs/demo.tape` and `docs/files.tape`.

## Releases

Pushing a `v*` tag (for example `v2.8.0`) builds binaries for Linux, macOS and
Windows with [GoReleaser](https://goreleaser.com) and attaches them to a
GitHub release.

By contributing you agree that your contributions are licensed under the
[MIT License](LICENSE).
