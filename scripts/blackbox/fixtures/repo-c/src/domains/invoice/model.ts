import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one billing invoice. */
export interface Invoice {
  readonly id: string;
  readonly tenantId: string;
  status: InvoiceStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly InvoiceChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface InvoiceChange {
  readonly at: string;
  readonly from: InvoiceStatus;
  readonly to: InvoiceStatus;
}

export type InvoiceStatus = "draft" | "active" | "settled" | "cancelled";

export const invoiceStatuses: readonly InvoiceStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a billing invoice; anything else is rejected upstream. */
const transitions: Record<InvoiceStatus, readonly InvoiceStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canInvoiceTransition(from: InvoiceStatus, to: InvoiceStatus): boolean {
  return transitions[from].includes(to);
}

export function isInvoiceTerminal(value: Invoice): boolean {
  return transitions[value.status].length === 0;
}

export function newInvoice(id: string, tenantId: string, reference: string): Invoice {
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

export function touchInvoice(value: Invoice): Invoice {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyInvoiceTransition(value: Invoice, to: InvoiceStatus): Invoice {
  const change: InvoiceChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withInvoiceAmount(value: Invoice, amountCents: number): Invoice {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("invoice amount must be a non-negative integer");
  }
  return touchInvoice({ ...value, amountCents });
}

export function withInvoiceLabel(value: Invoice, label: string): Invoice {
  if (label.trim().length === 0) {
    throw new ValidationError("invoice label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchInvoice({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutInvoiceLabel(value: Invoice, label: string): Invoice {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchInvoice({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateInvoice(value: Invoice): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("invoice requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("invoice reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("invoice amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("invoice updatedAt precedes createdAt");
  }
}

export function compareInvoice(left: Invoice, right: Invoice): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeInvoice(value: Invoice): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function invoiceStatusCounts(values: readonly Invoice[]): Record<InvoiceStatus, number> {
  const counts: Record<InvoiceStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
