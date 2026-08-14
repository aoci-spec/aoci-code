import { Contact, ContactStatus, summarizeContact } from "./model";
import { ContactPage } from "./repository";
import { ContactSummary } from "./service";

export interface ContactPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface ContactPagePayload {
  items: readonly ContactPayload[];
  total: number;
  offset: number;
}

export interface ContactSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<ContactStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a contact person; tenant identity never leaves the service. */
export function toContactPayload(value: Contact): ContactPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeContact(value),
    updatedAt: value.updatedAt,
  };
}

export function toContactPayloads(values: readonly Contact[]): ContactPayload[] {
  return values.map(toContactPayload);
}

export function toContactPagePayload(page: ContactPage): ContactPagePayload {
  return { items: toContactPayloads(page.items), total: page.total, offset: page.offset };
}

export function toContactSummaryPayload(summary: ContactSummary): ContactSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Contact[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toContactCsvRow(value: Contact): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
