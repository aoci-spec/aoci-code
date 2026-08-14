import { Template, TemplateStatus, summarizeTemplate } from "./model";
import { TemplatePage } from "./repository";
import { TemplateSummary } from "./service";

export interface TemplatePayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface TemplatePagePayload {
  items: readonly TemplatePayload[];
  total: number;
  offset: number;
}

export interface TemplateSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<TemplateStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a message template; tenant identity never leaves the service. */
export function toTemplatePayload(value: Template): TemplatePayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeTemplate(value),
    updatedAt: value.updatedAt,
  };
}

export function toTemplatePayloads(values: readonly Template[]): TemplatePayload[] {
  return values.map(toTemplatePayload);
}

export function toTemplatePagePayload(page: TemplatePage): TemplatePagePayload {
  return { items: toTemplatePayloads(page.items), total: page.total, offset: page.offset };
}

export function toTemplateSummaryPayload(summary: TemplateSummary): TemplateSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Template[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toTemplateCsvRow(value: Template): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
