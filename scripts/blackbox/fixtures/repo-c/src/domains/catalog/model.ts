import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one product catalog. */
export interface Catalog {
  readonly id: string;
  readonly tenantId: string;
  status: CatalogStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly CatalogChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface CatalogChange {
  readonly at: string;
  readonly from: CatalogStatus;
  readonly to: CatalogStatus;
}

export type CatalogStatus = "draft" | "active" | "settled" | "cancelled";

export const catalogStatuses: readonly CatalogStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a product catalog; anything else is rejected upstream. */
const transitions: Record<CatalogStatus, readonly CatalogStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canCatalogTransition(from: CatalogStatus, to: CatalogStatus): boolean {
  return transitions[from].includes(to);
}

export function isCatalogTerminal(value: Catalog): boolean {
  return transitions[value.status].length === 0;
}

export function newCatalog(id: string, tenantId: string, reference: string): Catalog {
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

export function touchCatalog(value: Catalog): Catalog {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyCatalogTransition(value: Catalog, to: CatalogStatus): Catalog {
  const change: CatalogChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withCatalogAmount(value: Catalog, amountCents: number): Catalog {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("catalog amount must be a non-negative integer");
  }
  return touchCatalog({ ...value, amountCents });
}

export function withCatalogLabel(value: Catalog, label: string): Catalog {
  if (label.trim().length === 0) {
    throw new ValidationError("catalog label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchCatalog({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutCatalogLabel(value: Catalog, label: string): Catalog {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchCatalog({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateCatalog(value: Catalog): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("catalog requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("catalog reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("catalog amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("catalog updatedAt precedes createdAt");
  }
}

export function compareCatalog(left: Catalog, right: Catalog): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeCatalog(value: Catalog): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function catalogStatusCounts(values: readonly Catalog[]): Record<CatalogStatus, number> {
  const counts: Record<CatalogStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
