import {
  Payment,
  PaymentStatus,
  applyPaymentTransition,
  canPaymentTransition,
  isPaymentTerminal,
  newPayment,
  withPaymentAmount,
  withPaymentLabel,
  paymentStatusCounts,
} from "./model";
import { PaymentPage, PaymentRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface PaymentSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<PaymentStatus, number>;
}

/** Business rules for the payment attempt lifecycle. */
export class PaymentService {
  constructor(private readonly repository: PaymentRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Payment {
    const draft = withPaymentAmount(newPayment(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("payment.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Payment {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: PaymentStatus): Payment {
    const current = this.repository.require(tenantId, id);
    if (isPaymentTerminal(current)) {
      throw new IllegalTransitionError(`payment ${id} is terminal`);
    }
    if (!canPaymentTransition(current.status, next)) {
      throw new IllegalTransitionError(`payment ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyPaymentTransition(current, next));
    auditEvent("payment.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Payment {
    const current = this.repository.require(tenantId, id);
    if (isPaymentTerminal(current)) {
      throw new IllegalTransitionError(`payment ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`payment ${id} cannot fall below zero`);
    }
    return this.repository.save(withPaymentAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Payment {
    return this.repository.save(withPaymentLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyPaymentTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("payment.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Payment[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): PaymentPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): PaymentSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: paymentStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isPaymentTerminal(current)) {
      throw new IllegalTransitionError(`payment ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("payment.discarded", { tenantId, id });
  }
}
