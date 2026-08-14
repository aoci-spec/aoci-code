import { Invoice, InvoiceStatus, summarizeInvoice } from "./model";
import { InvoicePage } from "./repository";
import { InvoiceSummary } from "./service";

export interface InvoicePayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface InvoicePagePayload {
  items: readonly InvoicePayload[];
  total: number;
  offset: number;
}

export interface InvoiceSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<InvoiceStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a billing invoice; tenant identity never leaves the service. */
export function toInvoicePayload(value: Invoice): InvoicePayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeInvoice(value),
    updatedAt: value.updatedAt,
  };
}

export function toInvoicePayloads(values: readonly Invoice[]): InvoicePayload[] {
  return values.map(toInvoicePayload);
}

export function toInvoicePagePayload(page: InvoicePage): InvoicePagePayload {
  return { items: toInvoicePayloads(page.items), total: page.total, offset: page.offset };
}

export function toInvoiceSummaryPayload(summary: InvoiceSummary): InvoiceSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Invoice[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toInvoiceCsvRow(value: Invoice): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
