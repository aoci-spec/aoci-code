import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one bulk export run. */
export interface ExportRun {
  readonly id: string;
  readonly tenantId: string;
  status: ExportRunStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly ExportRunChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface ExportRunChange {
  readonly at: string;
  readonly from: ExportRunStatus;
  readonly to: ExportRunStatus;
}

export type ExportRunStatus = "draft" | "active" | "settled" | "cancelled";

export const exportStatuses: readonly ExportRunStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a bulk export run; anything else is rejected upstream. */
const transitions: Record<ExportRunStatus, readonly ExportRunStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canExportRunTransition(from: ExportRunStatus, to: ExportRunStatus): boolean {
  return transitions[from].includes(to);
}

export function isExportRunTerminal(value: ExportRun): boolean {
  return transitions[value.status].length === 0;
}

export function newExportRun(id: string, tenantId: string, reference: string): ExportRun {
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

export function touchExportRun(value: ExportRun): ExportRun {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyExportRunTransition(value: ExportRun, to: ExportRunStatus): ExportRun {
  const change: ExportRunChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withExportRunAmount(value: ExportRun, amountCents: number): ExportRun {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("export amount must be a non-negative integer");
  }
  return touchExportRun({ ...value, amountCents });
}

export function withExportRunLabel(value: ExportRun, label: string): ExportRun {
  if (label.trim().length === 0) {
    throw new ValidationError("export label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchExportRun({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutExportRunLabel(value: ExportRun, label: string): ExportRun {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchExportRun({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateExportRun(value: ExportRun): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("export requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("export reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("export amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("export updatedAt precedes createdAt");
  }
}

export function compareExportRun(left: ExportRun, right: ExportRun): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeExportRun(value: ExportRun): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function exportStatusCounts(values: readonly ExportRun[]): Record<ExportRunStatus, number> {
  const counts: Record<ExportRunStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
