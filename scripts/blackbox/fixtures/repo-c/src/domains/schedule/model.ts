import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one scheduled run. */
export interface Schedule {
  readonly id: string;
  readonly tenantId: string;
  status: ScheduleStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly ScheduleChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface ScheduleChange {
  readonly at: string;
  readonly from: ScheduleStatus;
  readonly to: ScheduleStatus;
}

export type ScheduleStatus = "draft" | "active" | "settled" | "cancelled";

export const scheduleStatuses: readonly ScheduleStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a scheduled run; anything else is rejected upstream. */
const transitions: Record<ScheduleStatus, readonly ScheduleStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canScheduleTransition(from: ScheduleStatus, to: ScheduleStatus): boolean {
  return transitions[from].includes(to);
}

export function isScheduleTerminal(value: Schedule): boolean {
  return transitions[value.status].length === 0;
}

export function newSchedule(id: string, tenantId: string, reference: string): Schedule {
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

export function touchSchedule(value: Schedule): Schedule {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyScheduleTransition(value: Schedule, to: ScheduleStatus): Schedule {
  const change: ScheduleChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withScheduleAmount(value: Schedule, amountCents: number): Schedule {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("schedule amount must be a non-negative integer");
  }
  return touchSchedule({ ...value, amountCents });
}

export function withScheduleLabel(value: Schedule, label: string): Schedule {
  if (label.trim().length === 0) {
    throw new ValidationError("schedule label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchSchedule({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutScheduleLabel(value: Schedule, label: string): Schedule {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchSchedule({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateSchedule(value: Schedule): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("schedule requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("schedule reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("schedule amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("schedule updatedAt precedes createdAt");
  }
}

export function compareSchedule(left: Schedule, right: Schedule): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeSchedule(value: Schedule): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function scheduleStatusCounts(values: readonly Schedule[]): Record<ScheduleStatus, number> {
  const counts: Record<ScheduleStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
