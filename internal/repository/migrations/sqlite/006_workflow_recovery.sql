ALTER TABLE work_items ADD COLUMN workflow_max_task_instances_per_node INTEGER NOT NULL DEFAULT 0 CHECK (workflow_max_task_instances_per_node BETWEEN 0 AND 500);
