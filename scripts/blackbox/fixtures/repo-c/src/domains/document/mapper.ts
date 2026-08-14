import { Document, DocumentStatus, summarizeDocument } from "./model";
import { DocumentPage } from "./repository";
import { DocumentSummary } from "./service";

export interface DocumentPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface DocumentPagePayload {
  items: readonly DocumentPayload[];
  total: number;
  offset: number;
}

export interface DocumentSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<DocumentStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a stored document; tenant identity never leaves the service. */
export function toDocumentPayload(value: Document): DocumentPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeDocument(value),
    updatedAt: value.updatedAt,
  };
}

export function toDocumentPayloads(values: readonly Document[]): DocumentPayload[] {
  return values.map(toDocumentPayload);
}

export function toDocumentPagePayload(page: DocumentPage): DocumentPagePayload {
  return { items: toDocumentPayloads(page.items), total: page.total, offset: page.offset };
}

export function toDocumentSummaryPayload(summary: DocumentSummary): DocumentSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Document[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toDocumentCsvRow(value: Document): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
