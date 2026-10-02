CREATE TABLE host_state (
    host_id         INTEGER PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
    last_local_dir  TEXT NOT NULL DEFAULT '',
    last_remote_dir TEXT NOT NULL DEFAULT ''
);
