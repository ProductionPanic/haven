# Security

haven manages access to your servers, so security problems matter. If you
find one, please **do not open a public issue**. Report it privately through
GitHub's [private vulnerability reporting](https://github.com/ProductionPanic/haven/security/advisories/new)
instead. You'll get a reply within a few working days.

Things that are in scope:

- haven running something on a host, or changing a file there, that the user
  didn't ask for.
- The production guards (`--allow-production`, the extra confirmations) being
  bypassed.
- Secrets leaking into output that is meant to be shared, such as
  `haven servers` or `haven llm`.

## What haven stores

- Hosts live in a local SQLite database (`~/.config/haven/haven.db`), in a
  directory only you can open (`0700`). It holds names, users, hostnames, ports, paths
  to identity files, notes and tags. **It never stores private keys or
  passwords.** Don't put secrets in the notes field either.
- All connections go through your own `ssh` binary, so authentication, host
  key checking and agents work exactly as they do with plain `ssh`.
