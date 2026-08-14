import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one audit trail record. */
export interface Audit {
  readonly id: string;
  readonly tenantId: string;
  status: AuditStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly AuditChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface AuditChange {
  readonly at: string;
  readonly from: AuditStatus;
  readonly to: AuditStatus;
}

export type AuditStatus = "draft" | "active" | "settled" | "cancelled";

export const auditStatuses: readonly AuditStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a audit trail record; anything else is rejected upstream. */
const transitions: Record<AuditStatus, readonly AuditStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canAuditTransition(from: AuditStatus, to: AuditStatus): boolean {
  return transitions[from].includes(to);
}

export function isAuditTerminal(value: Audit): boolean {
  return transitions[value.status].length === 0;
}

export function newAudit(id: string, tenantId: string, reference: string): Audit {
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

export function touchAudit(value: Audit): Audit {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyAuditTransition(value: Audit, to: AuditStatus): Audit {
  const change: AuditChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withAuditAmount(value: Audit, amountCents: number): Audit {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("audit amount must be a non-negative integer");
  }
  return touchAudit({ ...value, amountCents });
}

export function withAuditLabel(value: Audit, label: string): Audit {
  if (label.trim().length === 0) {
    throw new ValidationError("audit label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchAudit({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutAuditLabel(value: Audit, label: string): Audit {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchAudit({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateAudit(value: Audit): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("audit requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("audit reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("audit amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("audit updatedAt precedes createdAt");
  }
}

export function compareAudit(left: Audit, right: Audit): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeAudit(value: Audit): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function auditStatusCounts(values: readonly Audit[]): Record<AuditStatus, number> {
  const counts: Record<AuditStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
