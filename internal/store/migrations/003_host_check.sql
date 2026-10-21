ALTER TABLE host_state ADD COLUMN last_check_at    TIMESTAMP;
ALTER TABLE host_state ADD COLUMN last_check_ok    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE host_state ADD COLUMN last_check_error TEXT NOT NULL DEFAULT '';
