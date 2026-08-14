import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one price definition. */
export interface Price {
  readonly id: string;
  readonly tenantId: string;
  status: PriceStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly PriceChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface PriceChange {
  readonly at: string;
  readonly from: PriceStatus;
  readonly to: PriceStatus;
}

export type PriceStatus = "draft" | "active" | "settled" | "cancelled";

export const priceStatuses: readonly PriceStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a price definition; anything else is rejected upstream. */
const transitions: Record<PriceStatus, readonly PriceStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canPriceTransition(from: PriceStatus, to: PriceStatus): boolean {
  return transitions[from].includes(to);
}

export function isPriceTerminal(value: Price): boolean {
  return transitions[value.status].length === 0;
}

export function newPrice(id: string, tenantId: string, reference: string): Price {
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

export function touchPrice(value: Price): Price {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyPriceTransition(value: Price, to: PriceStatus): Price {
  const change: PriceChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withPriceAmount(value: Price, amountCents: number): Price {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("price amount must be a non-negative integer");
  }
  return touchPrice({ ...value, amountCents });
}

export function withPriceLabel(value: Price, label: string): Price {
  if (label.trim().length === 0) {
    throw new ValidationError("price label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchPrice({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutPriceLabel(value: Price, label: string): Price {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchPrice({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validatePrice(value: Price): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("price requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("price reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("price amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("price updatedAt precedes createdAt");
  }
}

export function comparePrice(left: Price, right: Price): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizePrice(value: Price): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function priceStatusCounts(values: readonly Price[]): Record<PriceStatus, number> {
  const counts: Record<PriceStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
