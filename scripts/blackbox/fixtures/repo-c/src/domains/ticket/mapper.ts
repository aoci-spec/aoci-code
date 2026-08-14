import { Ticket, TicketStatus, summarizeTicket } from "./model";
import { TicketPage } from "./repository";
import { TicketSummary } from "./service";

export interface TicketPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface TicketPagePayload {
  items: readonly TicketPayload[];
  total: number;
  offset: number;
}

export interface TicketSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<TicketStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a support ticket; tenant identity never leaves the service. */
export function toTicketPayload(value: Ticket): TicketPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeTicket(value),
    updatedAt: value.updatedAt,
  };
}

export function toTicketPayloads(values: readonly Ticket[]): TicketPayload[] {
  return values.map(toTicketPayload);
}

export function toTicketPagePayload(page: TicketPage): TicketPagePayload {
  return { items: toTicketPayloads(page.items), total: page.total, offset: page.offset };
}

export function toTicketSummaryPayload(summary: TicketSummary): TicketSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Ticket[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toTicketCsvRow(value: Ticket): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
