import {
  Approval,
  ApprovalStatus,
  applyApprovalTransition,
  canApprovalTransition,
  isApprovalTerminal,
  newApproval,
  withApprovalAmount,
  withApprovalLabel,
  approvalStatusCounts,
} from "./model";
import { ApprovalPage, ApprovalRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface ApprovalSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<ApprovalStatus, number>;
}

/** Business rules for the approval decision lifecycle. */
export class ApprovalService {
  constructor(private readonly repository: ApprovalRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Approval {
    const draft = withApprovalAmount(newApproval(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("approval.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Approval {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: ApprovalStatus): Approval {
    const current = this.repository.require(tenantId, id);
    if (isApprovalTerminal(current)) {
      throw new IllegalTransitionError(`approval ${id} is terminal`);
    }
    if (!canApprovalTransition(current.status, next)) {
      throw new IllegalTransitionError(`approval ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyApprovalTransition(current, next));
    auditEvent("approval.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Approval {
    const current = this.repository.require(tenantId, id);
    if (isApprovalTerminal(current)) {
      throw new IllegalTransitionError(`approval ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`approval ${id} cannot fall below zero`);
    }
    return this.repository.save(withApprovalAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Approval {
    return this.repository.save(withApprovalLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyApprovalTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("approval.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Approval[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): ApprovalPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): ApprovalSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: approvalStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isApprovalTerminal(current)) {
      throw new IllegalTransitionError(`approval ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("approval.discarded", { tenantId, id });
  }
}
