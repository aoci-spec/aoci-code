import { Alert, AlertStatus, summarizeAlert } from "./model";
import { AlertPage } from "./repository";
import { AlertSummary } from "./service";

export interface AlertPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface AlertPagePayload {
  items: readonly AlertPayload[];
  total: number;
  offset: number;
}

export interface AlertSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<AlertStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a operational alert; tenant identity never leaves the service. */
export function toAlertPayload(value: Alert): AlertPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeAlert(value),
    updatedAt: value.updatedAt,
  };
}

export function toAlertPayloads(values: readonly Alert[]): AlertPayload[] {
  return values.map(toAlertPayload);
}

export function toAlertPagePayload(page: AlertPage): AlertPagePayload {
  return { items: toAlertPayloads(page.items), total: page.total, offset: page.offset };
}

export function toAlertSummaryPayload(summary: AlertSummary): AlertSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Alert[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toAlertCsvRow(value: Alert): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
