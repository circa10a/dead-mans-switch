package database

// schema defines the table and indexes
const schema = `
CREATE TABLE IF NOT EXISTS switches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message TEXT NOT NULL,
    notifiers TEXT NOT NULL,
    send_at TEXT NOT NULL,
    sent BOOLEAN DEFAULT 0,
    check_in_interval TEXT NOT NULL,
    delete_after_sent BOOLEAN DEFAULT 0,
    disabled BOOLEAN DEFAULT 0,
    encrypted BOOLEAN DEFAULT 0,
    push_subscription TEXT,
    reminder_threshold TEXT,
    reminder_sent BOOLEAN DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_pending_active_switches ON switches (sent, disabled, send_at);
`
