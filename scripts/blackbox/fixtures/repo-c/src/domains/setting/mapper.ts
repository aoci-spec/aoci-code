import { Setting, SettingStatus, summarizeSetting } from "./model";
import { SettingPage } from "./repository";
import { SettingSummary } from "./service";

export interface SettingPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface SettingPagePayload {
  items: readonly SettingPayload[];
  total: number;
  offset: number;
}

export interface SettingSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<SettingStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a tenant setting; tenant identity never leaves the service. */
export function toSettingPayload(value: Setting): SettingPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeSetting(value),
    updatedAt: value.updatedAt,
  };
}

export function toSettingPayloads(values: readonly Setting[]): SettingPayload[] {
  return values.map(toSettingPayload);
}

export function toSettingPagePayload(page: SettingPage): SettingPagePayload {
  return { items: toSettingPayloads(page.items), total: page.total, offset: page.offset };
}

export function toSettingSummaryPayload(summary: SettingSummary): SettingSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Setting[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toSettingCsvRow(value: Setting): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
