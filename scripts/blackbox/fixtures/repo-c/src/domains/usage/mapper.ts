import { Usage, UsageStatus, summarizeUsage } from "./model";
import { UsagePage } from "./repository";
import { UsageSummary } from "./service";

export interface UsagePayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface UsagePagePayload {
  items: readonly UsagePayload[];
  total: number;
  offset: number;
}

export interface UsageSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<UsageStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a metered usage record; tenant identity never leaves the service. */
export function toUsagePayload(value: Usage): UsagePayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeUsage(value),
    updatedAt: value.updatedAt,
  };
}

export function toUsagePayloads(values: readonly Usage[]): UsagePayload[] {
  return values.map(toUsagePayload);
}

export function toUsagePagePayload(page: UsagePage): UsagePagePayload {
  return { items: toUsagePayloads(page.items), total: page.total, offset: page.offset };
}

export function toUsageSummaryPayload(summary: UsageSummary): UsageSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Usage[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toUsageCsvRow(value: Usage): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
