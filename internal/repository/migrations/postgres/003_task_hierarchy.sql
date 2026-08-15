ALTER TABLE tasks
    ADD COLUMN parent_task_id TEXT REFERENCES tasks (id);
-- +kairos StatementBreak
CREATE INDEX tasks_parent_position_idx
    ON tasks (work_item_id, parent_task_id, position, id);
