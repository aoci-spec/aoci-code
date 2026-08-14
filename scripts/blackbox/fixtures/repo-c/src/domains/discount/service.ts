import {
  Discount,
  DiscountStatus,
  applyDiscountTransition,
  canDiscountTransition,
  isDiscountTerminal,
  newDiscount,
  withDiscountAmount,
  withDiscountLabel,
  discountStatusCounts,
} from "./model";
import { DiscountPage, DiscountRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface DiscountSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<DiscountStatus, number>;
}

/** Business rules for the discount rule lifecycle. */
export class DiscountService {
  constructor(private readonly repository: DiscountRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Discount {
    const draft = withDiscountAmount(newDiscount(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("discount.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Discount {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: DiscountStatus): Discount {
    const current = this.repository.require(tenantId, id);
    if (isDiscountTerminal(current)) {
      throw new IllegalTransitionError(`discount ${id} is terminal`);
    }
    if (!canDiscountTransition(current.status, next)) {
      throw new IllegalTransitionError(`discount ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyDiscountTransition(current, next));
    auditEvent("discount.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Discount {
    const current = this.repository.require(tenantId, id);
    if (isDiscountTerminal(current)) {
      throw new IllegalTransitionError(`discount ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`discount ${id} cannot fall below zero`);
    }
    return this.repository.save(withDiscountAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Discount {
    return this.repository.save(withDiscountLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyDiscountTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("discount.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Discount[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): DiscountPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): DiscountSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: discountStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isDiscountTerminal(current)) {
      throw new IllegalTransitionError(`discount ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("discount.discarded", { tenantId, id });
  }
}
