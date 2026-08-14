import { ImportRun, ImportRunStatus, summarizeImportRun } from "./model";
import { ImportRunPage } from "./repository";
import { ImportRunSummary } from "./service";

export interface ImportRunPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface ImportRunPagePayload {
  items: readonly ImportRunPayload[];
  total: number;
  offset: number;
}

export interface ImportRunSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<ImportRunStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a bulk import run; tenant identity never leaves the service. */
export function toImportRunPayload(value: ImportRun): ImportRunPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeImportRun(value),
    updatedAt: value.updatedAt,
  };
}

export function toImportRunPayloads(values: readonly ImportRun[]): ImportRunPayload[] {
  return values.map(toImportRunPayload);
}

export function toImportRunPagePayload(page: ImportRunPage): ImportRunPagePayload {
  return { items: toImportRunPayloads(page.items), total: page.total, offset: page.offset };
}

export function toImportRunSummaryPayload(summary: ImportRunSummary): ImportRunSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly ImportRun[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toImportRunCsvRow(value: ImportRun): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
