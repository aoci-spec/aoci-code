import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one sellable product. */
export interface Product {
  readonly id: string;
  readonly tenantId: string;
  status: ProductStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly ProductChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface ProductChange {
  readonly at: string;
  readonly from: ProductStatus;
  readonly to: ProductStatus;
}

export type ProductStatus = "draft" | "active" | "settled" | "cancelled";

export const productStatuses: readonly ProductStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a sellable product; anything else is rejected upstream. */
const transitions: Record<ProductStatus, readonly ProductStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canProductTransition(from: ProductStatus, to: ProductStatus): boolean {
  return transitions[from].includes(to);
}

export function isProductTerminal(value: Product): boolean {
  return transitions[value.status].length === 0;
}

export function newProduct(id: string, tenantId: string, reference: string): Product {
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

export function touchProduct(value: Product): Product {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyProductTransition(value: Product, to: ProductStatus): Product {
  const change: ProductChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withProductAmount(value: Product, amountCents: number): Product {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("product amount must be a non-negative integer");
  }
  return touchProduct({ ...value, amountCents });
}

export function withProductLabel(value: Product, label: string): Product {
  if (label.trim().length === 0) {
    throw new ValidationError("product label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchProduct({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutProductLabel(value: Product, label: string): Product {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchProduct({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateProduct(value: Product): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("product requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("product reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("product amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("product updatedAt precedes createdAt");
  }
}

export function compareProduct(left: Product, right: Product): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeProduct(value: Product): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function productStatusCounts(values: readonly Product[]): Record<ProductStatus, number> {
  const counts: Record<ProductStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
