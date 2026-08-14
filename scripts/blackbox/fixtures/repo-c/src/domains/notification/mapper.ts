import { Notification, NotificationStatus, summarizeNotification } from "./model";
import { NotificationPage } from "./repository";
import { NotificationSummary } from "./service";

export interface NotificationPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface NotificationPagePayload {
  items: readonly NotificationPayload[];
  total: number;
  offset: number;
}

export interface NotificationSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<NotificationStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a outbound notification; tenant identity never leaves the service. */
export function toNotificationPayload(value: Notification): NotificationPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeNotification(value),
    updatedAt: value.updatedAt,
  };
}

export function toNotificationPayloads(values: readonly Notification[]): NotificationPayload[] {
  return values.map(toNotificationPayload);
}

export function toNotificationPagePayload(page: NotificationPage): NotificationPagePayload {
  return { items: toNotificationPayloads(page.items), total: page.total, offset: page.offset };
}

export function toNotificationSummaryPayload(summary: NotificationSummary): NotificationSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Notification[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toNotificationCsvRow(value: Notification): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
