CREATE TABLE hosts (
    id            INTEGER PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE COLLATE NOCASE,
    user          TEXT NOT NULL DEFAULT '',
    hostname      TEXT NOT NULL,
    port          INTEGER NOT NULL DEFAULT 22,
    identity_file TEXT NOT NULL DEFAULT '',
    jump_host     TEXT NOT NULL DEFAULT '',
    remote_path   TEXT NOT NULL DEFAULT '',
    extra_args    TEXT NOT NULL DEFAULT '',
    notes         TEXT NOT NULL DEFAULT '',
    environment   TEXT NOT NULL DEFAULT '',
    last_used_at  DATETIME,
    use_count     INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tags (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE host_tags (
    host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE,
    tag_id  INTEGER REFERENCES tags(id)  ON DELETE CASCADE,
    PRIMARY KEY (host_id, tag_id)
);

CREATE TABLE bookmarks (
    id      INTEGER PRIMARY KEY,
    host_id INTEGER REFERENCES hosts(id) ON DELETE CASCADE,
    side    TEXT NOT NULL CHECK (side IN ('local', 'remote')),
    path    TEXT NOT NULL
);
