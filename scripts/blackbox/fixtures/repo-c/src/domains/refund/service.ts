import {
  Refund,
  RefundStatus,
  applyRefundTransition,
  canRefundTransition,
  isRefundTerminal,
  newRefund,
  withRefundAmount,
  withRefundLabel,
  refundStatusCounts,
} from "./model";
import { RefundPage, RefundRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface RefundSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<RefundStatus, number>;
}

/** Business rules for the refund request lifecycle. */
export class RefundService {
  constructor(private readonly repository: RefundRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Refund {
    const draft = withRefundAmount(newRefund(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("refund.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Refund {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: RefundStatus): Refund {
    const current = this.repository.require(tenantId, id);
    if (isRefundTerminal(current)) {
      throw new IllegalTransitionError(`refund ${id} is terminal`);
    }
    if (!canRefundTransition(current.status, next)) {
      throw new IllegalTransitionError(`refund ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyRefundTransition(current, next));
    auditEvent("refund.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Refund {
    const current = this.repository.require(tenantId, id);
    if (isRefundTerminal(current)) {
      throw new IllegalTransitionError(`refund ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`refund ${id} cannot fall below zero`);
    }
    return this.repository.save(withRefundAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Refund {
    return this.repository.save(withRefundLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyRefundTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("refund.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Refund[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): RefundPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): RefundSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: refundStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isRefundTerminal(current)) {
      throw new IllegalTransitionError(`refund ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("refund.discarded", { tenantId, id });
  }
}
