import { Customer, CustomerStatus, summarizeCustomer } from "./model";
import { CustomerPage } from "./repository";
import { CustomerSummary } from "./service";

export interface CustomerPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface CustomerPagePayload {
  items: readonly CustomerPayload[];
  total: number;
  offset: number;
}

export interface CustomerSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<CustomerStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a customer account; tenant identity never leaves the service. */
export function toCustomerPayload(value: Customer): CustomerPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeCustomer(value),
    updatedAt: value.updatedAt,
  };
}

export function toCustomerPayloads(values: readonly Customer[]): CustomerPayload[] {
  return values.map(toCustomerPayload);
}

export function toCustomerPagePayload(page: CustomerPage): CustomerPagePayload {
  return { items: toCustomerPayloads(page.items), total: page.total, offset: page.offset };
}

export function toCustomerSummaryPayload(summary: CustomerSummary): CustomerSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Customer[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toCustomerCsvRow(value: Customer): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
