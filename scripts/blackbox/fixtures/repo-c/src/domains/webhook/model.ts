import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one outbound webhook. */
export interface Webhook {
  readonly id: string;
  readonly tenantId: string;
  status: WebhookStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly WebhookChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface WebhookChange {
  readonly at: string;
  readonly from: WebhookStatus;
  readonly to: WebhookStatus;
}

export type WebhookStatus = "draft" | "active" | "settled" | "cancelled";

export const webhookStatuses: readonly WebhookStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a outbound webhook; anything else is rejected upstream. */
const transitions: Record<WebhookStatus, readonly WebhookStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canWebhookTransition(from: WebhookStatus, to: WebhookStatus): boolean {
  return transitions[from].includes(to);
}

export function isWebhookTerminal(value: Webhook): boolean {
  return transitions[value.status].length === 0;
}

export function newWebhook(id: string, tenantId: string, reference: string): Webhook {
  const now = isoTimestamp();
  return {
    id,
    tenantId,
    status: "draft",
    amountCents: 0,
    reference,
    labels: [],
    history: [],
    createdAt: now,
    updatedAt: now,
  };
}

export function touchWebhook(value: Webhook): Webhook {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyWebhookTransition(value: Webhook, to: WebhookStatus): Webhook {
  const change: WebhookChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withWebhookAmount(value: Webhook, amountCents: number): Webhook {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("webhook amount must be a non-negative integer");
  }
  return touchWebhook({ ...value, amountCents });
}

export function withWebhookLabel(value: Webhook, label: string): Webhook {
  if (label.trim().length === 0) {
    throw new ValidationError("webhook label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchWebhook({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutWebhookLabel(value: Webhook, label: string): Webhook {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchWebhook({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateWebhook(value: Webhook): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("webhook requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("webhook reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("webhook amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("webhook updatedAt precedes createdAt");
  }
}

export function compareWebhook(left: Webhook, right: Webhook): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeWebhook(value: Webhook): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function webhookStatusCounts(values: readonly Webhook[]): Record<WebhookStatus, number> {
  const counts: Record<WebhookStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
