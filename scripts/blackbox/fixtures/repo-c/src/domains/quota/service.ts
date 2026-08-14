import {
  Quota,
  QuotaStatus,
  applyQuotaTransition,
  canQuotaTransition,
  isQuotaTerminal,
  newQuota,
  withQuotaAmount,
  withQuotaLabel,
  quotaStatusCounts,
} from "./model";
import { QuotaPage, QuotaRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface QuotaSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<QuotaStatus, number>;
}

/** Business rules for the consumption quota lifecycle. */
export class QuotaService {
  constructor(private readonly repository: QuotaRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Quota {
    const draft = withQuotaAmount(newQuota(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("quota.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Quota {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: QuotaStatus): Quota {
    const current = this.repository.require(tenantId, id);
    if (isQuotaTerminal(current)) {
      throw new IllegalTransitionError(`quota ${id} is terminal`);
    }
    if (!canQuotaTransition(current.status, next)) {
      throw new IllegalTransitionError(`quota ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyQuotaTransition(current, next));
    auditEvent("quota.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Quota {
    const current = this.repository.require(tenantId, id);
    if (isQuotaTerminal(current)) {
      throw new IllegalTransitionError(`quota ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`quota ${id} cannot fall below zero`);
    }
    return this.repository.save(withQuotaAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Quota {
    return this.repository.save(withQuotaLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyQuotaTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("quota.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Quota[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): QuotaPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): QuotaSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: quotaStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isQuotaTerminal(current)) {
      throw new IllegalTransitionError(`quota ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("quota.discarded", { tenantId, id });
  }
}
