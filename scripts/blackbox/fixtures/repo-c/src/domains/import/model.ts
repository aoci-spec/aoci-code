import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one bulk import run. */
export interface ImportRun {
  readonly id: string;
  readonly tenantId: string;
  status: ImportRunStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly ImportRunChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface ImportRunChange {
  readonly at: string;
  readonly from: ImportRunStatus;
  readonly to: ImportRunStatus;
}

export type ImportRunStatus = "draft" | "active" | "settled" | "cancelled";

export const importStatuses: readonly ImportRunStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a bulk import run; anything else is rejected upstream. */
const transitions: Record<ImportRunStatus, readonly ImportRunStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canImportRunTransition(from: ImportRunStatus, to: ImportRunStatus): boolean {
  return transitions[from].includes(to);
}

export function isImportRunTerminal(value: ImportRun): boolean {
  return transitions[value.status].length === 0;
}

export function newImportRun(id: string, tenantId: string, reference: string): ImportRun {
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

export function touchImportRun(value: ImportRun): ImportRun {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyImportRunTransition(value: ImportRun, to: ImportRunStatus): ImportRun {
  const change: ImportRunChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withImportRunAmount(value: ImportRun, amountCents: number): ImportRun {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("import amount must be a non-negative integer");
  }
  return touchImportRun({ ...value, amountCents });
}

export function withImportRunLabel(value: ImportRun, label: string): ImportRun {
  if (label.trim().length === 0) {
    throw new ValidationError("import label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchImportRun({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutImportRunLabel(value: ImportRun, label: string): ImportRun {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchImportRun({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateImportRun(value: ImportRun): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("import requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("import reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("import amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("import updatedAt precedes createdAt");
  }
}

export function compareImportRun(left: ImportRun, right: ImportRun): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeImportRun(value: ImportRun): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function importStatusCounts(values: readonly ImportRun[]): Record<ImportRunStatus, number> {
  const counts: Record<ImportRunStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
