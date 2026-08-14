import { Plan, PlanStatus, summarizePlan } from "./model";
import { PlanPage } from "./repository";
import { PlanSummary } from "./service";

export interface PlanPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface PlanPagePayload {
  items: readonly PlanPayload[];
  total: number;
  offset: number;
}

export interface PlanSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<PlanStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a subscription plan; tenant identity never leaves the service. */
export function toPlanPayload(value: Plan): PlanPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizePlan(value),
    updatedAt: value.updatedAt,
  };
}

export function toPlanPayloads(values: readonly Plan[]): PlanPayload[] {
  return values.map(toPlanPayload);
}

export function toPlanPagePayload(page: PlanPage): PlanPagePayload {
  return { items: toPlanPayloads(page.items), total: page.total, offset: page.offset };
}

export function toPlanSummaryPayload(summary: PlanSummary): PlanSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Plan[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toPlanCsvRow(value: Plan): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
