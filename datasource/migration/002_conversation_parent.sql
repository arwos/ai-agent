ALTER TABLE conversations ADD COLUMN parent_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS conversations_parent_id ON conversations(profile_id, parent_id);
