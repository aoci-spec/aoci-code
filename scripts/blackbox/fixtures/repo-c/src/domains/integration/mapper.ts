import { Integration, IntegrationStatus, summarizeIntegration } from "./model";
import { IntegrationPage } from "./repository";
import { IntegrationSummary } from "./service";

export interface IntegrationPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface IntegrationPagePayload {
  items: readonly IntegrationPayload[];
  total: number;
  offset: number;
}

export interface IntegrationSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<IntegrationStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a external integration; tenant identity never leaves the service. */
export function toIntegrationPayload(value: Integration): IntegrationPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeIntegration(value),
    updatedAt: value.updatedAt,
  };
}

export function toIntegrationPayloads(values: readonly Integration[]): IntegrationPayload[] {
  return values.map(toIntegrationPayload);
}

export function toIntegrationPagePayload(page: IntegrationPage): IntegrationPagePayload {
  return { items: toIntegrationPayloads(page.items), total: page.total, offset: page.offset };
}

export function toIntegrationSummaryPayload(summary: IntegrationSummary): IntegrationSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Integration[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toIntegrationCsvRow(value: Integration): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
