import { Order, OrderStatus, summarizeOrder } from "./model";
import { OrderPage } from "./repository";
import { OrderSummary } from "./service";

export interface OrderPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface OrderPagePayload {
  items: readonly OrderPayload[];
  total: number;
  offset: number;
}

export interface OrderSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<OrderStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a purchase order; tenant identity never leaves the service. */
export function toOrderPayload(value: Order): OrderPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeOrder(value),
    updatedAt: value.updatedAt,
  };
}

export function toOrderPayloads(values: readonly Order[]): OrderPayload[] {
  return values.map(toOrderPayload);
}

export function toOrderPagePayload(page: OrderPage): OrderPagePayload {
  return { items: toOrderPayloads(page.items), total: page.total, offset: page.offset };
}

export function toOrderSummaryPayload(summary: OrderSummary): OrderSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Order[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toOrderCsvRow(value: Order): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
