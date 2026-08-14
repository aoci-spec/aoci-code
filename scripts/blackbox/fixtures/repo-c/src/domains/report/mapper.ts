import { Report, ReportStatus, summarizeReport } from "./model";
import { ReportPage } from "./repository";
import { ReportSummary } from "./service";

export interface ReportPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface ReportPagePayload {
  items: readonly ReportPayload[];
  total: number;
  offset: number;
}

export interface ReportSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<ReportStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a generated report; tenant identity never leaves the service. */
export function toReportPayload(value: Report): ReportPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeReport(value),
    updatedAt: value.updatedAt,
  };
}

export function toReportPayloads(values: readonly Report[]): ReportPayload[] {
  return values.map(toReportPayload);
}

export function toReportPagePayload(page: ReportPage): ReportPagePayload {
  return { items: toReportPayloads(page.items), total: page.total, offset: page.offset };
}

export function toReportSummaryPayload(summary: ReportSummary): ReportSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Report[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toReportCsvRow(value: Report): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
