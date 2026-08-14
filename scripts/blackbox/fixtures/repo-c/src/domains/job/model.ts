import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one background job. */
export interface Job {
  readonly id: string;
  readonly tenantId: string;
  status: JobStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly JobChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface JobChange {
  readonly at: string;
  readonly from: JobStatus;
  readonly to: JobStatus;
}

export type JobStatus = "draft" | "active" | "settled" | "cancelled";

export const jobStatuses: readonly JobStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a background job; anything else is rejected upstream. */
const transitions: Record<JobStatus, readonly JobStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canJobTransition(from: JobStatus, to: JobStatus): boolean {
  return transitions[from].includes(to);
}

export function isJobTerminal(value: Job): boolean {
  return transitions[value.status].length === 0;
}

export function newJob(id: string, tenantId: string, reference: string): Job {
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

export function touchJob(value: Job): Job {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyJobTransition(value: Job, to: JobStatus): Job {
  const change: JobChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withJobAmount(value: Job, amountCents: number): Job {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("job amount must be a non-negative integer");
  }
  return touchJob({ ...value, amountCents });
}

export function withJobLabel(value: Job, label: string): Job {
  if (label.trim().length === 0) {
    throw new ValidationError("job label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchJob({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutJobLabel(value: Job, label: string): Job {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchJob({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateJob(value: Job): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("job requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("job reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("job amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("job updatedAt precedes createdAt");
  }
}

export function compareJob(left: Job, right: Job): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeJob(value: Job): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function jobStatusCounts(values: readonly Job[]): Record<JobStatus, number> {
  const counts: Record<JobStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
