import { Fraud, FraudStatus, summarizeFraud } from "./model";
import { FraudPage } from "./repository";
import { FraudSummary } from "./service";

export interface FraudPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface FraudPagePayload {
  items: readonly FraudPayload[];
  total: number;
  offset: number;
}

export interface FraudSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<FraudStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a fraud signal; tenant identity never leaves the service. */
export function toFraudPayload(value: Fraud): FraudPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeFraud(value),
    updatedAt: value.updatedAt,
  };
}

export function toFraudPayloads(values: readonly Fraud[]): FraudPayload[] {
  return values.map(toFraudPayload);
}

export function toFraudPagePayload(page: FraudPage): FraudPagePayload {
  return { items: toFraudPayloads(page.items), total: page.total, offset: page.offset };
}

export function toFraudSummaryPayload(summary: FraudSummary): FraudSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Fraud[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toFraudCsvRow(value: Fraud): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
