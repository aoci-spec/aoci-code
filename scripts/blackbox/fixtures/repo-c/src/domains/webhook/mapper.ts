import { Webhook, WebhookStatus, summarizeWebhook } from "./model";
import { WebhookPage } from "./repository";
import { WebhookSummary } from "./service";

export interface WebhookPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface WebhookPagePayload {
  items: readonly WebhookPayload[];
  total: number;
  offset: number;
}

export interface WebhookSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<WebhookStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a outbound webhook; tenant identity never leaves the service. */
export function toWebhookPayload(value: Webhook): WebhookPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeWebhook(value),
    updatedAt: value.updatedAt,
  };
}

export function toWebhookPayloads(values: readonly Webhook[]): WebhookPayload[] {
  return values.map(toWebhookPayload);
}

export function toWebhookPagePayload(page: WebhookPage): WebhookPagePayload {
  return { items: toWebhookPayloads(page.items), total: page.total, offset: page.offset };
}

export function toWebhookSummaryPayload(summary: WebhookSummary): WebhookSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Webhook[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toWebhookCsvRow(value: Webhook): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
