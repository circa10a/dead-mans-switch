package database

const schema = `
CREATE TABLE IF NOT EXISTS switches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    check_in_interval TEXT NOT NULL,
    delete_after_triggered BOOLEAN DEFAULT 0,
    encrypted BOOLEAN DEFAULT 0,
    failure_reason TEXT,
    message TEXT NOT NULL,
    notifiers TEXT NOT NULL,
    push_subscription TEXT,
    reminder_enabled BOOLEAN DEFAULT 0,
    reminder_sent BOOLEAN DEFAULT 0,
    reminder_threshold TEXT,
    status TEXT NOT NULL,
    trigger_at INTEGER DEFAULT 0,
    user_id TEXT NOT NULL DEFAULT 'admin'
);

CREATE INDEX IF NOT EXISTS idx_pending_active_switches ON switches (user_id, status, trigger_at);
`
