import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one external integration. */
export interface Integration {
  readonly id: string;
  readonly tenantId: string;
  status: IntegrationStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly IntegrationChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface IntegrationChange {
  readonly at: string;
  readonly from: IntegrationStatus;
  readonly to: IntegrationStatus;
}

export type IntegrationStatus = "draft" | "active" | "settled" | "cancelled";

export const integrationStatuses: readonly IntegrationStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a external integration; anything else is rejected upstream. */
const transitions: Record<IntegrationStatus, readonly IntegrationStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canIntegrationTransition(from: IntegrationStatus, to: IntegrationStatus): boolean {
  return transitions[from].includes(to);
}

export function isIntegrationTerminal(value: Integration): boolean {
  return transitions[value.status].length === 0;
}

export function newIntegration(id: string, tenantId: string, reference: string): Integration {
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

export function touchIntegration(value: Integration): Integration {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyIntegrationTransition(value: Integration, to: IntegrationStatus): Integration {
  const change: IntegrationChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withIntegrationAmount(value: Integration, amountCents: number): Integration {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("integration amount must be a non-negative integer");
  }
  return touchIntegration({ ...value, amountCents });
}

export function withIntegrationLabel(value: Integration, label: string): Integration {
  if (label.trim().length === 0) {
    throw new ValidationError("integration label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchIntegration({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutIntegrationLabel(value: Integration, label: string): Integration {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchIntegration({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateIntegration(value: Integration): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("integration requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("integration reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("integration amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("integration updatedAt precedes createdAt");
  }
}

export function compareIntegration(left: Integration, right: Integration): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeIntegration(value: Integration): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function integrationStatusCounts(values: readonly Integration[]): Record<IntegrationStatus, number> {
  const counts: Record<IntegrationStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
