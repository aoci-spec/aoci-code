import { Tracking, TrackingStatus, summarizeTracking } from "./model";
import { TrackingPage } from "./repository";
import { TrackingSummary } from "./service";

export interface TrackingPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface TrackingPagePayload {
  items: readonly TrackingPayload[];
  total: number;
  offset: number;
}

export interface TrackingSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<TrackingStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a tracking event; tenant identity never leaves the service. */
export function toTrackingPayload(value: Tracking): TrackingPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeTracking(value),
    updatedAt: value.updatedAt,
  };
}

export function toTrackingPayloads(values: readonly Tracking[]): TrackingPayload[] {
  return values.map(toTrackingPayload);
}

export function toTrackingPagePayload(page: TrackingPage): TrackingPagePayload {
  return { items: toTrackingPayloads(page.items), total: page.total, offset: page.offset };
}

export function toTrackingSummaryPayload(summary: TrackingSummary): TrackingSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Tracking[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toTrackingCsvRow(value: Tracking): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
