import { Session, SessionStatus, summarizeSession } from "./model";
import { SessionPage } from "./repository";
import { SessionSummary } from "./service";

export interface SessionPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface SessionPagePayload {
  items: readonly SessionPayload[];
  total: number;
  offset: number;
}

export interface SessionSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<SessionStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a authenticated session; tenant identity never leaves the service. */
export function toSessionPayload(value: Session): SessionPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeSession(value),
    updatedAt: value.updatedAt,
  };
}

export function toSessionPayloads(values: readonly Session[]): SessionPayload[] {
  return values.map(toSessionPayload);
}

export function toSessionPagePayload(page: SessionPage): SessionPagePayload {
  return { items: toSessionPayloads(page.items), total: page.total, offset: page.offset };
}

export function toSessionSummaryPayload(summary: SessionSummary): SessionSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Session[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toSessionCsvRow(value: Session): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
