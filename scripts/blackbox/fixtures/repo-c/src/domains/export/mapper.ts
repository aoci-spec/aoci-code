import { ExportRun, ExportRunStatus, summarizeExportRun } from "./model";
import { ExportRunPage } from "./repository";
import { ExportRunSummary } from "./service";

export interface ExportRunPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface ExportRunPagePayload {
  items: readonly ExportRunPayload[];
  total: number;
  offset: number;
}

export interface ExportRunSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<ExportRunStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a bulk export run; tenant identity never leaves the service. */
export function toExportRunPayload(value: ExportRun): ExportRunPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeExportRun(value),
    updatedAt: value.updatedAt,
  };
}

export function toExportRunPayloads(values: readonly ExportRun[]): ExportRunPayload[] {
  return values.map(toExportRunPayload);
}

export function toExportRunPagePayload(page: ExportRunPage): ExportRunPagePayload {
  return { items: toExportRunPayloads(page.items), total: page.total, offset: page.offset };
}

export function toExportRunSummaryPayload(summary: ExportRunSummary): ExportRunSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly ExportRun[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toExportRunCsvRow(value: ExportRun): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
