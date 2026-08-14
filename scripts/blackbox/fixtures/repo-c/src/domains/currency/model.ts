import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one currency rate. */
export interface Currency {
  readonly id: string;
  readonly tenantId: string;
  status: CurrencyStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly CurrencyChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface CurrencyChange {
  readonly at: string;
  readonly from: CurrencyStatus;
  readonly to: CurrencyStatus;
}

export type CurrencyStatus = "draft" | "active" | "settled" | "cancelled";

export const currencyStatuses: readonly CurrencyStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a currency rate; anything else is rejected upstream. */
const transitions: Record<CurrencyStatus, readonly CurrencyStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canCurrencyTransition(from: CurrencyStatus, to: CurrencyStatus): boolean {
  return transitions[from].includes(to);
}

export function isCurrencyTerminal(value: Currency): boolean {
  return transitions[value.status].length === 0;
}

export function newCurrency(id: string, tenantId: string, reference: string): Currency {
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

export function touchCurrency(value: Currency): Currency {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyCurrencyTransition(value: Currency, to: CurrencyStatus): Currency {
  const change: CurrencyChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withCurrencyAmount(value: Currency, amountCents: number): Currency {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("currency amount must be a non-negative integer");
  }
  return touchCurrency({ ...value, amountCents });
}

export function withCurrencyLabel(value: Currency, label: string): Currency {
  if (label.trim().length === 0) {
    throw new ValidationError("currency label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchCurrency({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutCurrencyLabel(value: Currency, label: string): Currency {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchCurrency({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateCurrency(value: Currency): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("currency requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("currency reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("currency amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("currency updatedAt precedes createdAt");
  }
}

export function compareCurrency(left: Currency, right: Currency): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeCurrency(value: Currency): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function currencyStatusCounts(values: readonly Currency[]): Record<CurrencyStatus, number> {
  const counts: Record<CurrencyStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
