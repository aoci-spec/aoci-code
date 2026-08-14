import { Attachment, AttachmentStatus, summarizeAttachment } from "./model";
import { AttachmentPage } from "./repository";
import { AttachmentSummary } from "./service";

export interface AttachmentPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface AttachmentPagePayload {
  items: readonly AttachmentPayload[];
  total: number;
  offset: number;
}

export interface AttachmentSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<AttachmentStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a file attachment; tenant identity never leaves the service. */
export function toAttachmentPayload(value: Attachment): AttachmentPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeAttachment(value),
    updatedAt: value.updatedAt,
  };
}

export function toAttachmentPayloads(values: readonly Attachment[]): AttachmentPayload[] {
  return values.map(toAttachmentPayload);
}

export function toAttachmentPagePayload(page: AttachmentPage): AttachmentPagePayload {
  return { items: toAttachmentPayloads(page.items), total: page.total, offset: page.offset };
}

export function toAttachmentSummaryPayload(summary: AttachmentSummary): AttachmentSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Attachment[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toAttachmentCsvRow(value: Attachment): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
