import { Message, MessageStatus, summarizeMessage } from "./model";
import { MessagePage } from "./repository";
import { MessageSummary } from "./service";

export interface MessagePayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface MessagePagePayload {
  items: readonly MessagePayload[];
  total: number;
  offset: number;
}

export interface MessageSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<MessageStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a customer message; tenant identity never leaves the service. */
export function toMessagePayload(value: Message): MessagePayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeMessage(value),
    updatedAt: value.updatedAt,
  };
}

export function toMessagePayloads(values: readonly Message[]): MessagePayload[] {
  return values.map(toMessagePayload);
}

export function toMessagePagePayload(page: MessagePage): MessagePagePayload {
  return { items: toMessagePayloads(page.items), total: page.total, offset: page.offset };
}

export function toMessageSummaryPayload(summary: MessageSummary): MessageSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Message[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toMessageCsvRow(value: Message): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
