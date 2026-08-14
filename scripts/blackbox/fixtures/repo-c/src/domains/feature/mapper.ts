import { Feature, FeatureStatus, summarizeFeature } from "./model";
import { FeaturePage } from "./repository";
import { FeatureSummary } from "./service";

export interface FeaturePayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface FeaturePagePayload {
  items: readonly FeaturePayload[];
  total: number;
  offset: number;
}

export interface FeatureSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<FeatureStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a feature flag; tenant identity never leaves the service. */
export function toFeaturePayload(value: Feature): FeaturePayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeFeature(value),
    updatedAt: value.updatedAt,
  };
}

export function toFeaturePayloads(values: readonly Feature[]): FeaturePayload[] {
  return values.map(toFeaturePayload);
}

export function toFeaturePagePayload(page: FeaturePage): FeaturePagePayload {
  return { items: toFeaturePayloads(page.items), total: page.total, offset: page.offset };
}

export function toFeatureSummaryPayload(summary: FeatureSummary): FeatureSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Feature[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toFeatureCsvRow(value: Feature): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
