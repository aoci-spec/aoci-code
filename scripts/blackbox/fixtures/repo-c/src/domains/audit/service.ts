import {
  Audit,
  AuditStatus,
  applyAuditTransition,
  canAuditTransition,
  isAuditTerminal,
  newAudit,
  withAuditAmount,
  withAuditLabel,
  auditStatusCounts,
} from "./model";
import { AuditPage, AuditRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface AuditSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<AuditStatus, number>;
}

/** Business rules for the audit trail record lifecycle. */
export class AuditService {
  constructor(private readonly repository: AuditRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Audit {
    const draft = withAuditAmount(newAudit(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("audit.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Audit {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: AuditStatus): Audit {
    const current = this.repository.require(tenantId, id);
    if (isAuditTerminal(current)) {
      throw new IllegalTransitionError(`audit ${id} is terminal`);
    }
    if (!canAuditTransition(current.status, next)) {
      throw new IllegalTransitionError(`audit ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyAuditTransition(current, next));
    auditEvent("audit.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Audit {
    const current = this.repository.require(tenantId, id);
    if (isAuditTerminal(current)) {
      throw new IllegalTransitionError(`audit ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`audit ${id} cannot fall below zero`);
    }
    return this.repository.save(withAuditAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Audit {
    return this.repository.save(withAuditLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyAuditTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("audit.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Audit[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): AuditPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): AuditSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: auditStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isAuditTerminal(current)) {
      throw new IllegalTransitionError(`audit ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("audit.discarded", { tenantId, id });
  }
}
