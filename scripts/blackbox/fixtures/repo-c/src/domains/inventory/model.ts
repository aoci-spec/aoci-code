import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one stock position. */
export interface Inventory {
  readonly id: string;
  readonly tenantId: string;
  status: InventoryStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly InventoryChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface InventoryChange {
  readonly at: string;
  readonly from: InventoryStatus;
  readonly to: InventoryStatus;
}

export type InventoryStatus = "draft" | "active" | "settled" | "cancelled";

export const inventoryStatuses: readonly InventoryStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a stock position; anything else is rejected upstream. */
const transitions: Record<InventoryStatus, readonly InventoryStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canInventoryTransition(from: InventoryStatus, to: InventoryStatus): boolean {
  return transitions[from].includes(to);
}

export function isInventoryTerminal(value: Inventory): boolean {
  return transitions[value.status].length === 0;
}

export function newInventory(id: string, tenantId: string, reference: string): Inventory {
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

export function touchInventory(value: Inventory): Inventory {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyInventoryTransition(value: Inventory, to: InventoryStatus): Inventory {
  const change: InventoryChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withInventoryAmount(value: Inventory, amountCents: number): Inventory {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("inventory amount must be a non-negative integer");
  }
  return touchInventory({ ...value, amountCents });
}

export function withInventoryLabel(value: Inventory, label: string): Inventory {
  if (label.trim().length === 0) {
    throw new ValidationError("inventory label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchInventory({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutInventoryLabel(value: Inventory, label: string): Inventory {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchInventory({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateInventory(value: Inventory): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("inventory requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("inventory reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("inventory amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("inventory updatedAt precedes createdAt");
  }
}

export function compareInventory(left: Inventory, right: Inventory): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeInventory(value: Inventory): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function inventoryStatusCounts(values: readonly Inventory[]): Record<InventoryStatus, number> {
  const counts: Record<InventoryStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
