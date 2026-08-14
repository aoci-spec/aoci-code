import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one workflow task. */
export interface Task {
  readonly id: string;
  readonly tenantId: string;
  status: TaskStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly TaskChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface TaskChange {
  readonly at: string;
  readonly from: TaskStatus;
  readonly to: TaskStatus;
}

export type TaskStatus = "draft" | "active" | "settled" | "cancelled";

export const taskStatuses: readonly TaskStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a workflow task; anything else is rejected upstream. */
const transitions: Record<TaskStatus, readonly TaskStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canTaskTransition(from: TaskStatus, to: TaskStatus): boolean {
  return transitions[from].includes(to);
}

export function isTaskTerminal(value: Task): boolean {
  return transitions[value.status].length === 0;
}

export function newTask(id: string, tenantId: string, reference: string): Task {
  const now = isoTimestamp();
  return {
    id,
    tenantId,
    status: "draft",
    amountCents: 0,
    reference,
    labels: [],
    history: [],
    createdAt: now,
    updatedAt: now,
  };
}

export function touchTask(value: Task): Task {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyTaskTransition(value: Task, to: TaskStatus): Task {
  const change: TaskChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withTaskAmount(value: Task, amountCents: number): Task {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("task amount must be a non-negative integer");
  }
  return touchTask({ ...value, amountCents });
}

export function withTaskLabel(value: Task, label: string): Task {
  if (label.trim().length === 0) {
    throw new ValidationError("task label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchTask({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutTaskLabel(value: Task, label: string): Task {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchTask({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateTask(value: Task): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("task requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("task reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("task amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("task updatedAt precedes createdAt");
  }
}

export function compareTask(left: Task, right: Task): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeTask(value: Task): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function taskStatusCounts(values: readonly Task[]): Record<TaskStatus, number> {
  const counts: Record<TaskStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
