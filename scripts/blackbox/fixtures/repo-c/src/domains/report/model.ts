import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one generated report. */
export interface Report {
  readonly id: string;
  readonly tenantId: string;
  status: ReportStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly ReportChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface ReportChange {
  readonly at: string;
  readonly from: ReportStatus;
  readonly to: ReportStatus;
}

export type ReportStatus = "draft" | "active" | "settled" | "cancelled";

export const reportStatuses: readonly ReportStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a generated report; anything else is rejected upstream. */
const transitions: Record<ReportStatus, readonly ReportStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canReportTransition(from: ReportStatus, to: ReportStatus): boolean {
  return transitions[from].includes(to);
}

export function isReportTerminal(value: Report): boolean {
  return transitions[value.status].length === 0;
}

export function newReport(id: string, tenantId: string, reference: string): Report {
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

export function touchReport(value: Report): Report {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyReportTransition(value: Report, to: ReportStatus): Report {
  const change: ReportChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withReportAmount(value: Report, amountCents: number): Report {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("report amount must be a non-negative integer");
  }
  return touchReport({ ...value, amountCents });
}

export function withReportLabel(value: Report, label: string): Report {
  if (label.trim().length === 0) {
    throw new ValidationError("report label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchReport({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutReportLabel(value: Report, label: string): Report {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchReport({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateReport(value: Report): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("report requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("report reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("report amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("report updatedAt precedes createdAt");
  }
}

export function compareReport(left: Report, right: Report): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeReport(value: Report): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function reportStatusCounts(values: readonly Report[]): Record<ReportStatus, number> {
  const counts: Record<ReportStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
