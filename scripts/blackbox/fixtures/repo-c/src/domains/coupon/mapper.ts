import { Coupon, CouponStatus, summarizeCoupon } from "./model";
import { CouponPage } from "./repository";
import { CouponSummary } from "./service";

export interface CouponPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface CouponPagePayload {
  items: readonly CouponPayload[];
  total: number;
  offset: number;
}

export interface CouponSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<CouponStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a coupon grant; tenant identity never leaves the service. */
export function toCouponPayload(value: Coupon): CouponPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeCoupon(value),
    updatedAt: value.updatedAt,
  };
}

export function toCouponPayloads(values: readonly Coupon[]): CouponPayload[] {
  return values.map(toCouponPayload);
}

export function toCouponPagePayload(page: CouponPage): CouponPagePayload {
  return { items: toCouponPayloads(page.items), total: page.total, offset: page.offset };
}

export function toCouponSummaryPayload(summary: CouponSummary): CouponSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Coupon[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toCouponCsvRow(value: Coupon): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
