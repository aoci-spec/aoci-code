import {
  Coupon,
  CouponStatus,
  applyCouponTransition,
  canCouponTransition,
  isCouponTerminal,
  newCoupon,
  withCouponAmount,
  withCouponLabel,
  couponStatusCounts,
} from "./model";
import { CouponPage, CouponRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface CouponSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<CouponStatus, number>;
}

/** Business rules for the coupon grant lifecycle. */
export class CouponService {
  constructor(private readonly repository: CouponRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Coupon {
    const draft = withCouponAmount(newCoupon(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("coupon.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Coupon {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: CouponStatus): Coupon {
    const current = this.repository.require(tenantId, id);
    if (isCouponTerminal(current)) {
      throw new IllegalTransitionError(`coupon ${id} is terminal`);
    }
    if (!canCouponTransition(current.status, next)) {
      throw new IllegalTransitionError(`coupon ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyCouponTransition(current, next));
    auditEvent("coupon.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Coupon {
    const current = this.repository.require(tenantId, id);
    if (isCouponTerminal(current)) {
      throw new IllegalTransitionError(`coupon ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`coupon ${id} cannot fall below zero`);
    }
    return this.repository.save(withCouponAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Coupon {
    return this.repository.save(withCouponLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyCouponTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("coupon.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Coupon[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): CouponPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): CouponSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: couponStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isCouponTerminal(current)) {
      throw new IllegalTransitionError(`coupon ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("coupon.discarded", { tenantId, id });
  }
}
