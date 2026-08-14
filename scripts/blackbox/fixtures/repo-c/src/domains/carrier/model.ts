import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one delivery carrier. */
export interface Carrier {
  readonly id: string;
  readonly tenantId: string;
  status: CarrierStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly CarrierChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface CarrierChange {
  readonly at: string;
  readonly from: CarrierStatus;
  readonly to: CarrierStatus;
}

export type CarrierStatus = "draft" | "active" | "settled" | "cancelled";

export const carrierStatuses: readonly CarrierStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a delivery carrier; anything else is rejected upstream. */
const transitions: Record<CarrierStatus, readonly CarrierStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canCarrierTransition(from: CarrierStatus, to: CarrierStatus): boolean {
  return transitions[from].includes(to);
}

export function isCarrierTerminal(value: Carrier): boolean {
  return transitions[value.status].length === 0;
}

export function newCarrier(id: string, tenantId: string, reference: string): Carrier {
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

export function touchCarrier(value: Carrier): Carrier {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyCarrierTransition(value: Carrier, to: CarrierStatus): Carrier {
  const change: CarrierChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withCarrierAmount(value: Carrier, amountCents: number): Carrier {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("carrier amount must be a non-negative integer");
  }
  return touchCarrier({ ...value, amountCents });
}

export function withCarrierLabel(value: Carrier, label: string): Carrier {
  if (label.trim().length === 0) {
    throw new ValidationError("carrier label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchCarrier({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutCarrierLabel(value: Carrier, label: string): Carrier {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchCarrier({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateCarrier(value: Carrier): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("carrier requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("carrier reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("carrier amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("carrier updatedAt precedes createdAt");
  }
}

export function compareCarrier(left: Carrier, right: Carrier): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeCarrier(value: Carrier): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function carrierStatusCounts(values: readonly Carrier[]): Record<CarrierStatus, number> {
  const counts: Record<CarrierStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
