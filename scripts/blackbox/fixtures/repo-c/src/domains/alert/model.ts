import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one operational alert. */
export interface Alert {
  readonly id: string;
  readonly tenantId: string;
  status: AlertStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly AlertChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface AlertChange {
  readonly at: string;
  readonly from: AlertStatus;
  readonly to: AlertStatus;
}

export type AlertStatus = "draft" | "active" | "settled" | "cancelled";

export const alertStatuses: readonly AlertStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a operational alert; anything else is rejected upstream. */
const transitions: Record<AlertStatus, readonly AlertStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canAlertTransition(from: AlertStatus, to: AlertStatus): boolean {
  return transitions[from].includes(to);
}

export function isAlertTerminal(value: Alert): boolean {
  return transitions[value.status].length === 0;
}

export function newAlert(id: string, tenantId: string, reference: string): Alert {
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

export function touchAlert(value: Alert): Alert {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyAlertTransition(value: Alert, to: AlertStatus): Alert {
  const change: AlertChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withAlertAmount(value: Alert, amountCents: number): Alert {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("alert amount must be a non-negative integer");
  }
  return touchAlert({ ...value, amountCents });
}

export function withAlertLabel(value: Alert, label: string): Alert {
  if (label.trim().length === 0) {
    throw new ValidationError("alert label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchAlert({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutAlertLabel(value: Alert, label: string): Alert {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchAlert({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateAlert(value: Alert): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("alert requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("alert reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("alert amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("alert updatedAt precedes createdAt");
  }
}

export function compareAlert(left: Alert, right: Alert): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeAlert(value: Alert): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function alertStatusCounts(values: readonly Alert[]): Record<AlertStatus, number> {
  const counts: Record<AlertStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
