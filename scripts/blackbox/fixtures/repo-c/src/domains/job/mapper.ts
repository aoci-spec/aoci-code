import { Job, JobStatus, summarizeJob } from "./model";
import { JobPage } from "./repository";
import { JobSummary } from "./service";

export interface JobPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface JobPagePayload {
  items: readonly JobPayload[];
  total: number;
  offset: number;
}

export interface JobSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<JobStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a background job; tenant identity never leaves the service. */
export function toJobPayload(value: Job): JobPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeJob(value),
    updatedAt: value.updatedAt,
  };
}

export function toJobPayloads(values: readonly Job[]): JobPayload[] {
  return values.map(toJobPayload);
}

export function toJobPagePayload(page: JobPage): JobPagePayload {
  return { items: toJobPayloads(page.items), total: page.total, offset: page.offset };
}

export function toJobSummaryPayload(summary: JobSummary): JobSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Job[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toJobCsvRow(value: Job): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
