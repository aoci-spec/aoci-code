import { Payment, PaymentStatus, summarizePayment } from "./model";
import { PaymentPage } from "./repository";
import { PaymentSummary } from "./service";

export interface PaymentPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface PaymentPagePayload {
  items: readonly PaymentPayload[];
  total: number;
  offset: number;
}

export interface PaymentSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<PaymentStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a payment attempt; tenant identity never leaves the service. */
export function toPaymentPayload(value: Payment): PaymentPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizePayment(value),
    updatedAt: value.updatedAt,
  };
}

export function toPaymentPayloads(values: readonly Payment[]): PaymentPayload[] {
  return values.map(toPaymentPayload);
}

export function toPaymentPagePayload(page: PaymentPage): PaymentPagePayload {
  return { items: toPaymentPayloads(page.items), total: page.total, offset: page.offset };
}

export function toPaymentSummaryPayload(summary: PaymentSummary): PaymentSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Payment[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toPaymentCsvRow(value: Payment): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
