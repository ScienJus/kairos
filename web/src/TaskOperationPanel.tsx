import type { ReactNode } from "react";
import { ChevronDown } from "lucide-react";

export type TaskOperation =
  | "start"
  | "complete"
  | "release"
  | "fail"
  | "review"
  | "decompose"
  | "add-child"
  | "skip";

export function TaskOperationPanel({
  operation,
  activeOperation,
  onSelect,
  title,
  children,
  danger = false,
}: {
  operation: TaskOperation;
  activeOperation: TaskOperation | null;
  onSelect: (operation: TaskOperation | null) => void;
  title: string;
  children: ReactNode;
  danger?: boolean;
}) {
  const open = operation === activeOperation;
  return (
    <section
      className={`operation-panel ${open ? "open" : ""} ${danger ? "danger" : ""}`}
    >
      <button
        type="button"
        aria-expanded={open}
        onClick={() => onSelect(open ? null : operation)}
      >
        <span>{title}</span>
        <ChevronDown size={16} />
      </button>
      {open && <div className="operation-content">{children}</div>}
    </section>
  );
}
