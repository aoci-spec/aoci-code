import { Task, TaskStatus, summarizeTask } from "./model";
import { TaskPage } from "./repository";
import { TaskSummary } from "./service";

export interface TaskPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface TaskPagePayload {
  items: readonly TaskPayload[];
  total: number;
  offset: number;
}

export interface TaskSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<TaskStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a workflow task; tenant identity never leaves the service. */
export function toTaskPayload(value: Task): TaskPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeTask(value),
    updatedAt: value.updatedAt,
  };
}

export function toTaskPayloads(values: readonly Task[]): TaskPayload[] {
  return values.map(toTaskPayload);
}

export function toTaskPagePayload(page: TaskPage): TaskPagePayload {
  return { items: toTaskPayloads(page.items), total: page.total, offset: page.offset };
}

export function toTaskSummaryPayload(summary: TaskSummary): TaskSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Task[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toTaskCsvRow(value: Task): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
