import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one postal address. */
export interface Address {
  readonly id: string;
  readonly tenantId: string;
  status: AddressStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly AddressChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface AddressChange {
  readonly at: string;
  readonly from: AddressStatus;
  readonly to: AddressStatus;
}

export type AddressStatus = "draft" | "active" | "settled" | "cancelled";

export const addressStatuses: readonly AddressStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a postal address; anything else is rejected upstream. */
const transitions: Record<AddressStatus, readonly AddressStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canAddressTransition(from: AddressStatus, to: AddressStatus): boolean {
  return transitions[from].includes(to);
}

export function isAddressTerminal(value: Address): boolean {
  return transitions[value.status].length === 0;
}

export function newAddress(id: string, tenantId: string, reference: string): Address {
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

export function touchAddress(value: Address): Address {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyAddressTransition(value: Address, to: AddressStatus): Address {
  const change: AddressChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withAddressAmount(value: Address, amountCents: number): Address {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("address amount must be a non-negative integer");
  }
  return touchAddress({ ...value, amountCents });
}

export function withAddressLabel(value: Address, label: string): Address {
  if (label.trim().length === 0) {
    throw new ValidationError("address label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchAddress({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutAddressLabel(value: Address, label: string): Address {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchAddress({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateAddress(value: Address): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("address requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("address reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("address amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("address updatedAt precedes createdAt");
  }
}

export function compareAddress(left: Address, right: Address): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeAddress(value: Address): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function addressStatusCounts(values: readonly Address[]): Record<AddressStatus, number> {
  const counts: Record<AddressStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
