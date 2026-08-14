import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one storage facility. */
export interface Warehouse {
  readonly id: string;
  readonly tenantId: string;
  status: WarehouseStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly WarehouseChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface WarehouseChange {
  readonly at: string;
  readonly from: WarehouseStatus;
  readonly to: WarehouseStatus;
}

export type WarehouseStatus = "draft" | "active" | "settled" | "cancelled";

export const warehouseStatuses: readonly WarehouseStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a storage facility; anything else is rejected upstream. */
const transitions: Record<WarehouseStatus, readonly WarehouseStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canWarehouseTransition(from: WarehouseStatus, to: WarehouseStatus): boolean {
  return transitions[from].includes(to);
}

export function isWarehouseTerminal(value: Warehouse): boolean {
  return transitions[value.status].length === 0;
}

export function newWarehouse(id: string, tenantId: string, reference: string): Warehouse {
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

export function touchWarehouse(value: Warehouse): Warehouse {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyWarehouseTransition(value: Warehouse, to: WarehouseStatus): Warehouse {
  const change: WarehouseChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withWarehouseAmount(value: Warehouse, amountCents: number): Warehouse {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("warehouse amount must be a non-negative integer");
  }
  return touchWarehouse({ ...value, amountCents });
}

export function withWarehouseLabel(value: Warehouse, label: string): Warehouse {
  if (label.trim().length === 0) {
    throw new ValidationError("warehouse label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchWarehouse({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutWarehouseLabel(value: Warehouse, label: string): Warehouse {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchWarehouse({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateWarehouse(value: Warehouse): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("warehouse requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("warehouse reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("warehouse amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("warehouse updatedAt precedes createdAt");
  }
}

export function compareWarehouse(left: Warehouse, right: Warehouse): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeWarehouse(value: Warehouse): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function warehouseStatusCounts(values: readonly Warehouse[]): Record<WarehouseStatus, number> {
  const counts: Record<WarehouseStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
