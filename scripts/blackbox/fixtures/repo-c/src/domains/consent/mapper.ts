import { Consent, ConsentStatus, summarizeConsent } from "./model";
import { ConsentPage } from "./repository";
import { ConsentSummary } from "./service";

export interface ConsentPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface ConsentPagePayload {
  items: readonly ConsentPayload[];
  total: number;
  offset: number;
}

export interface ConsentSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<ConsentStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a privacy consent; tenant identity never leaves the service. */
export function toConsentPayload(value: Consent): ConsentPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeConsent(value),
    updatedAt: value.updatedAt,
  };
}

export function toConsentPayloads(values: readonly Consent[]): ConsentPayload[] {
  return values.map(toConsentPayload);
}

export function toConsentPagePayload(page: ConsentPage): ConsentPagePayload {
  return { items: toConsentPayloads(page.items), total: page.total, offset: page.offset };
}

export function toConsentSummaryPayload(summary: ConsentSummary): ConsentSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Consent[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toConsentCsvRow(value: Consent): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
