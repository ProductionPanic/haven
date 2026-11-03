# haven for LLMs

haven keeps a list of the user's SSH servers. You can use it to find those
servers and to read and change files on them (or on the local disk) without
a terminal UI. Everything below is non-interactive: no prompts, no pickers.

## 1. Find the server

```
haven servers [query] [--tag TAG]
```

Prints a JSON array. Fields that matter:

- `name`: the server's name. Use it as `name:` in front of a path.
- `file_prefix`: that prefix, ready to paste (`appel:`).
- `production`: true for production hosts. Changing files there is guarded
  (see "Safety").
- `remote_path`: the site or project directory on the server, when set.
- `notes`, `tags`: context the user wrote down.
- `ssh_ok`: result of the last login check; `null` means never checked,
  `false` means logging in failed (see `check_error`).

A query must match exactly one host. If it matches several, haven fails and
lists them; use the exact `name` instead.

## 2. Address files: `host:path` or a local path

- `appel:/var/www/site/index.php`: absolute path on the server `appel`.
- `appel:wp-config.php`: relative paths start in the login directory
  (`~` and `~/x` work too).
- `./notes.txt`, `/tmp/x`, `~/x`: local paths. Anything without a `host:`
  prefix is local.

## 3. Commands

All `haven file` commands take `--json` for structured output. On failure
they print the reason to stderr and exit with status 1.

### List a directory

```
haven file list appel:/var/www/site [--json]
```

### Read a file

```
haven file read appel:/var/www/site/wp-config.php
haven file read appel:/var/log/app.log --offset 200 --limit 50 --numbered
```

Prints the content only (with `--json`: path, size, `start_line`, `end_line`,
`truncated`, `content`). Output stops after 1 MiB; a note on stderr tells you
which `--offset` to continue from. Binary files are refused.

### Create a file

```
haven file create appel:/tmp/hello.txt --content 'hello'
haven file create appel:/var/www/site/robots.txt <<'EOF'
User-agent: *
Disallow: /private/
EOF
```

Content comes from `--content` or stdin; use stdin (a heredoc) for anything
multi-line or with quotes. It fails if the file exists (`--force` replaces
it) or the parent directory is missing (`--parents` creates it). Empty
content is refused unless you pass `--empty`.

### Modify a file

Pick exactly one mode:

```
# replace text: --old must occur exactly once (or pass --all)
haven file modify appel:wp-config.php --old "'WP_DEBUG', false" --new "'WP_DEBUG', true"

# append (content from --content or stdin)
echo 'Disallow: /tmp/' | haven file modify appel:/var/www/site/robots.txt --append

# replace the whole file
haven file modify ./notes.txt --overwrite --content 'fresh start'
```

The file must exist. Permissions are kept. Binary files and files over
10 MiB are refused. If `--old` is not found or is not unique, nothing is
changed: read the file again and copy the text exactly, with more
surrounding lines if it must be unique.

### Delete

```
haven file delete appel:/tmp/old.log
haven file delete --recursive appel:/tmp/build-cache
```

Directories need `--recursive`. All paths are checked before anything is
deleted. The filesystem root and the home directory are never deleted.
(`haven rm` is something else: it removes a server from haven's list. Don't
use it unless the user asks.)

## 4. Safety

- Writes are atomic: data goes to `name.haven-part` and replaces the file
  only when complete.
- `create`, `modify` and `delete` on a **production** host fail with a clear
  message unless you add `--allow-production`. Only add that flag when the
  user explicitly asked for a change on that production server; say which
  server and file you are changing first.
- Never guess a server name or path. List first (`haven servers`,
  `haven file list`), then read, then change.
- Don't run `haven` without arguments or `haven files`: those open
  full-screen UIs that need a human.

## 5. Typical workflows

**Edit a setting on a server**

1. `haven servers appel` to confirm the name and whether it is production.
2. `haven file read appel:/var/www/site/.env --numbered` to see the file.
3. `haven file modify appel:/var/www/site/.env --old 'APP_DEBUG=true' --new 'APP_DEBUG=false'`
4. `haven file read appel:/var/www/site/.env` to verify.

**Copy whole files or directories between machines** with `haven cp`:

```
haven cp --overwrite ./dump.sql appel:/tmp/
haven cp appel:/var/www/site/wp-config.php ./backup/
```

Without `--overwrite`, `--skip`, `--newer` or `--rename`, `cp` skips files that
already exist and fails when it has no terminal to ask. `cp` has no production
guard of its own, so only copy to a production host when the user asked.

**Check that servers are reachable**

```
haven check --json          # exit status 1 when a login fails
```
