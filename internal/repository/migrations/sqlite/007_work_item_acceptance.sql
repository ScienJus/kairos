ALTER TABLE work_items ADD COLUMN acceptance_mode TEXT NOT NULL DEFAULT 'none' CHECK (acceptance_mode IN ('none', 'agent', 'human'));
