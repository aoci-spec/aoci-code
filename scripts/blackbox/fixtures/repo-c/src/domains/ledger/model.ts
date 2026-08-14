import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one accounting ledger entry. */
export interface Ledger {
  readonly id: string;
  readonly tenantId: string;
  status: LedgerStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly LedgerChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface LedgerChange {
  readonly at: string;
  readonly from: LedgerStatus;
  readonly to: LedgerStatus;
}

export type LedgerStatus = "draft" | "active" | "settled" | "cancelled";

export const ledgerStatuses: readonly LedgerStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a accounting ledger entry; anything else is rejected upstream. */
const transitions: Record<LedgerStatus, readonly LedgerStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canLedgerTransition(from: LedgerStatus, to: LedgerStatus): boolean {
  return transitions[from].includes(to);
}

export function isLedgerTerminal(value: Ledger): boolean {
  return transitions[value.status].length === 0;
}

export function newLedger(id: string, tenantId: string, reference: string): Ledger {
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

export function touchLedger(value: Ledger): Ledger {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyLedgerTransition(value: Ledger, to: LedgerStatus): Ledger {
  const change: LedgerChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withLedgerAmount(value: Ledger, amountCents: number): Ledger {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("ledger amount must be a non-negative integer");
  }
  return touchLedger({ ...value, amountCents });
}

export function withLedgerLabel(value: Ledger, label: string): Ledger {
  if (label.trim().length === 0) {
    throw new ValidationError("ledger label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchLedger({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutLedgerLabel(value: Ledger, label: string): Ledger {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchLedger({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateLedger(value: Ledger): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("ledger requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("ledger reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("ledger amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("ledger updatedAt precedes createdAt");
  }
}

export function compareLedger(left: Ledger, right: Ledger): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeLedger(value: Ledger): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function ledgerStatusCounts(values: readonly Ledger[]): Record<LedgerStatus, number> {
  const counts: Record<LedgerStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
