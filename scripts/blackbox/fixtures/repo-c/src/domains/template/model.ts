import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one message template. */
export interface Template {
  readonly id: string;
  readonly tenantId: string;
  status: TemplateStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly TemplateChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface TemplateChange {
  readonly at: string;
  readonly from: TemplateStatus;
  readonly to: TemplateStatus;
}

export type TemplateStatus = "draft" | "active" | "settled" | "cancelled";

export const templateStatuses: readonly TemplateStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a message template; anything else is rejected upstream. */
const transitions: Record<TemplateStatus, readonly TemplateStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canTemplateTransition(from: TemplateStatus, to: TemplateStatus): boolean {
  return transitions[from].includes(to);
}

export function isTemplateTerminal(value: Template): boolean {
  return transitions[value.status].length === 0;
}

export function newTemplate(id: string, tenantId: string, reference: string): Template {
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

export function touchTemplate(value: Template): Template {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyTemplateTransition(value: Template, to: TemplateStatus): Template {
  const change: TemplateChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withTemplateAmount(value: Template, amountCents: number): Template {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("template amount must be a non-negative integer");
  }
  return touchTemplate({ ...value, amountCents });
}

export function withTemplateLabel(value: Template, label: string): Template {
  if (label.trim().length === 0) {
    throw new ValidationError("template label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchTemplate({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutTemplateLabel(value: Template, label: string): Template {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchTemplate({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateTemplate(value: Template): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("template requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("template reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("template amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("template updatedAt precedes createdAt");
  }
}

export function compareTemplate(left: Template, right: Template): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeTemplate(value: Template): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function templateStatusCounts(values: readonly Template[]): Record<TemplateStatus, number> {
  const counts: Record<TemplateStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
