import { Journal, JournalStatus, summarizeJournal } from "./model";
import { JournalPage } from "./repository";
import { JournalSummary } from "./service";

export interface JournalPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface JournalPagePayload {
  items: readonly JournalPayload[];
  total: number;
  offset: number;
}

export interface JournalSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<JournalStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a journal batch; tenant identity never leaves the service. */
export function toJournalPayload(value: Journal): JournalPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeJournal(value),
    updatedAt: value.updatedAt,
  };
}

export function toJournalPayloads(values: readonly Journal[]): JournalPayload[] {
  return values.map(toJournalPayload);
}

export function toJournalPagePayload(page: JournalPage): JournalPagePayload {
  return { items: toJournalPayloads(page.items), total: page.total, offset: page.offset };
}

export function toJournalSummaryPayload(summary: JournalSummary): JournalSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Journal[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toJournalCsvRow(value: Journal): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
