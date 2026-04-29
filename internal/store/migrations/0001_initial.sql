CREATE TABLE IF NOT EXISTS mapping (
    type            TEXT    NOT NULL,
    hmac_full       BLOB    NOT NULL,
    index_truncated TEXT    NOT NULL,
    real_value      BLOB    NOT NULL,
    first_seen_at   INTEGER NOT NULL,
    PRIMARY KEY (type, hmac_full)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_mapping_token ON mapping (type, index_truncated);
CREATE INDEX IF NOT EXISTS idx_first_seen ON mapping (first_seen_at);
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);
