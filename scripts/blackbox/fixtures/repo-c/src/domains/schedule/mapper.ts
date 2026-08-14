import { Schedule, ScheduleStatus, summarizeSchedule } from "./model";
import { SchedulePage } from "./repository";
import { ScheduleSummary } from "./service";

export interface SchedulePayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface SchedulePagePayload {
  items: readonly SchedulePayload[];
  total: number;
  offset: number;
}

export interface ScheduleSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<ScheduleStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a scheduled run; tenant identity never leaves the service. */
export function toSchedulePayload(value: Schedule): SchedulePayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeSchedule(value),
    updatedAt: value.updatedAt,
  };
}

export function toSchedulePayloads(values: readonly Schedule[]): SchedulePayload[] {
  return values.map(toSchedulePayload);
}

export function toSchedulePagePayload(page: SchedulePage): SchedulePagePayload {
  return { items: toSchedulePayloads(page.items), total: page.total, offset: page.offset };
}

export function toScheduleSummaryPayload(summary: ScheduleSummary): ScheduleSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Schedule[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toScheduleCsvRow(value: Schedule): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
