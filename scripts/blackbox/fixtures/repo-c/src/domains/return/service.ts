import {
  ReturnCase,
  ReturnCaseStatus,
  applyReturnCaseTransition,
  canReturnCaseTransition,
  isReturnCaseTerminal,
  newReturnCase,
  withReturnCaseAmount,
  withReturnCaseLabel,
  returnStatusCounts,
} from "./model";
import { ReturnCasePage, ReturnCaseRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface ReturnCaseSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<ReturnCaseStatus, number>;
}

/** Business rules for the return case lifecycle. */
export class ReturnCaseService {
  constructor(private readonly repository: ReturnCaseRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): ReturnCase {
    const draft = withReturnCaseAmount(newReturnCase(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("return.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): ReturnCase {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: ReturnCaseStatus): ReturnCase {
    const current = this.repository.require(tenantId, id);
    if (isReturnCaseTerminal(current)) {
      throw new IllegalTransitionError(`return ${id} is terminal`);
    }
    if (!canReturnCaseTransition(current.status, next)) {
      throw new IllegalTransitionError(`return ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyReturnCaseTransition(current, next));
    auditEvent("return.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): ReturnCase {
    const current = this.repository.require(tenantId, id);
    if (isReturnCaseTerminal(current)) {
      throw new IllegalTransitionError(`return ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`return ${id} cannot fall below zero`);
    }
    return this.repository.save(withReturnCaseAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): ReturnCase {
    return this.repository.save(withReturnCaseLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyReturnCaseTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("return.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): ReturnCase[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): ReturnCasePage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): ReturnCaseSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: returnStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isReturnCaseTerminal(current)) {
      throw new IllegalTransitionError(`return ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("return.discarded", { tenantId, id });
  }
}
