package database

// schema defines the table and indexes
const schema = `
CREATE TABLE IF NOT EXISTS switches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    notifier TEXT NOT NULL,
    send_at TEXT NOT NULL,
    sent BOOLEAN DEFAULT 0,
    check_in_interval TEXT NOT NULL,
    delete_after_sent BOOLEAN DEFAULT 0,
    encrypted BOOLEAN DEFAULT 0
);

-- Index for the Worker to find pending switches quickly
CREATE INDEX IF NOT EXISTS idx_pending_switches ON switches (sent, send_at);
`
