import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one outbound shipment. */
export interface Shipment {
  readonly id: string;
  readonly tenantId: string;
  status: ShipmentStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly ShipmentChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface ShipmentChange {
  readonly at: string;
  readonly from: ShipmentStatus;
  readonly to: ShipmentStatus;
}

export type ShipmentStatus = "draft" | "active" | "settled" | "cancelled";

export const shipmentStatuses: readonly ShipmentStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a outbound shipment; anything else is rejected upstream. */
const transitions: Record<ShipmentStatus, readonly ShipmentStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canShipmentTransition(from: ShipmentStatus, to: ShipmentStatus): boolean {
  return transitions[from].includes(to);
}

export function isShipmentTerminal(value: Shipment): boolean {
  return transitions[value.status].length === 0;
}

export function newShipment(id: string, tenantId: string, reference: string): Shipment {
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

export function touchShipment(value: Shipment): Shipment {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyShipmentTransition(value: Shipment, to: ShipmentStatus): Shipment {
  const change: ShipmentChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withShipmentAmount(value: Shipment, amountCents: number): Shipment {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("shipment amount must be a non-negative integer");
  }
  return touchShipment({ ...value, amountCents });
}

export function withShipmentLabel(value: Shipment, label: string): Shipment {
  if (label.trim().length === 0) {
    throw new ValidationError("shipment label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchShipment({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutShipmentLabel(value: Shipment, label: string): Shipment {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchShipment({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateShipment(value: Shipment): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("shipment requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("shipment reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("shipment amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("shipment updatedAt precedes createdAt");
  }
}

export function compareShipment(left: Shipment, right: Shipment): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeShipment(value: Shipment): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function shipmentStatusCounts(values: readonly Shipment[]): Record<ShipmentStatus, number> {
  const counts: Record<ShipmentStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
