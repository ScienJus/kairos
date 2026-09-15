ALTER TABLE tasks ADD COLUMN retry_of_task_id TEXT REFERENCES tasks (id);
-- +kairos StatementBreak
CREATE UNIQUE INDEX tasks_retry_of_idx ON tasks (retry_of_task_id) WHERE retry_of_task_id IS NOT NULL;
-- +kairos StatementBreak
ALTER TABLE work_items ADD COLUMN restart_of_work_item_id TEXT REFERENCES work_items (id);
