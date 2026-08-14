import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one product variant. */
export interface Variant {
  readonly id: string;
  readonly tenantId: string;
  status: VariantStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly VariantChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface VariantChange {
  readonly at: string;
  readonly from: VariantStatus;
  readonly to: VariantStatus;
}

export type VariantStatus = "draft" | "active" | "settled" | "cancelled";

export const variantStatuses: readonly VariantStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a product variant; anything else is rejected upstream. */
const transitions: Record<VariantStatus, readonly VariantStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canVariantTransition(from: VariantStatus, to: VariantStatus): boolean {
  return transitions[from].includes(to);
}

export function isVariantTerminal(value: Variant): boolean {
  return transitions[value.status].length === 0;
}

export function newVariant(id: string, tenantId: string, reference: string): Variant {
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

export function touchVariant(value: Variant): Variant {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyVariantTransition(value: Variant, to: VariantStatus): Variant {
  const change: VariantChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withVariantAmount(value: Variant, amountCents: number): Variant {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("variant amount must be a non-negative integer");
  }
  return touchVariant({ ...value, amountCents });
}

export function withVariantLabel(value: Variant, label: string): Variant {
  if (label.trim().length === 0) {
    throw new ValidationError("variant label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchVariant({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutVariantLabel(value: Variant, label: string): Variant {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchVariant({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateVariant(value: Variant): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("variant requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("variant reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("variant amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("variant updatedAt precedes createdAt");
  }
}

export function compareVariant(left: Variant, right: Variant): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeVariant(value: Variant): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function variantStatusCounts(values: readonly Variant[]): Record<VariantStatus, number> {
  const counts: Record<VariantStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
