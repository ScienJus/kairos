ALTER TABLE work_items ADD COLUMN workflow_max_task_executions INTEGER NOT NULL DEFAULT 0 CHECK (workflow_max_task_executions BETWEEN 0 AND 500);
