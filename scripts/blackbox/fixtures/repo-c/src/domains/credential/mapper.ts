import { Credential, CredentialStatus, summarizeCredential } from "./model";
import { CredentialPage } from "./repository";
import { CredentialSummary } from "./service";

export interface CredentialPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface CredentialPagePayload {
  items: readonly CredentialPayload[];
  total: number;
  offset: number;
}

export interface CredentialSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<CredentialStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a stored credential; tenant identity never leaves the service. */
export function toCredentialPayload(value: Credential): CredentialPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeCredential(value),
    updatedAt: value.updatedAt,
  };
}

export function toCredentialPayloads(values: readonly Credential[]): CredentialPayload[] {
  return values.map(toCredentialPayload);
}

export function toCredentialPagePayload(page: CredentialPage): CredentialPagePayload {
  return { items: toCredentialPayloads(page.items), total: page.total, offset: page.offset };
}

export function toCredentialSummaryPayload(summary: CredentialSummary): CredentialSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Credential[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toCredentialCsvRow(value: Credential): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
