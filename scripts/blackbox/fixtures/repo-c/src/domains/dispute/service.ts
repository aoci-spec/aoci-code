import {
  Dispute,
  DisputeStatus,
  applyDisputeTransition,
  canDisputeTransition,
  isDisputeTerminal,
  newDispute,
  withDisputeAmount,
  withDisputeLabel,
  disputeStatusCounts,
} from "./model";
import { DisputePage, DisputeRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface DisputeSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<DisputeStatus, number>;
}

/** Business rules for the payment dispute lifecycle. */
export class DisputeService {
  constructor(private readonly repository: DisputeRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Dispute {
    const draft = withDisputeAmount(newDispute(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("dispute.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Dispute {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: DisputeStatus): Dispute {
    const current = this.repository.require(tenantId, id);
    if (isDisputeTerminal(current)) {
      throw new IllegalTransitionError(`dispute ${id} is terminal`);
    }
    if (!canDisputeTransition(current.status, next)) {
      throw new IllegalTransitionError(`dispute ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyDisputeTransition(current, next));
    auditEvent("dispute.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Dispute {
    const current = this.repository.require(tenantId, id);
    if (isDisputeTerminal(current)) {
      throw new IllegalTransitionError(`dispute ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`dispute ${id} cannot fall below zero`);
    }
    return this.repository.save(withDisputeAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Dispute {
    return this.repository.save(withDisputeLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyDisputeTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("dispute.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Dispute[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): DisputePage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): DisputeSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: disputeStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isDisputeTerminal(current)) {
      throw new IllegalTransitionError(`dispute ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("dispute.discarded", { tenantId, id });
  }
}
