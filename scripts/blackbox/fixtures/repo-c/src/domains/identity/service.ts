import {
  Identity,
  IdentityStatus,
  applyIdentityTransition,
  canIdentityTransition,
  isIdentityTerminal,
  newIdentity,
  withIdentityAmount,
  withIdentityLabel,
  identityStatusCounts,
} from "./model";
import { IdentityPage, IdentityRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface IdentitySummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<IdentityStatus, number>;
}

/** Business rules for the identity record lifecycle. */
export class IdentityService {
  constructor(private readonly repository: IdentityRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Identity {
    const draft = withIdentityAmount(newIdentity(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("identity.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Identity {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: IdentityStatus): Identity {
    const current = this.repository.require(tenantId, id);
    if (isIdentityTerminal(current)) {
      throw new IllegalTransitionError(`identity ${id} is terminal`);
    }
    if (!canIdentityTransition(current.status, next)) {
      throw new IllegalTransitionError(`identity ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyIdentityTransition(current, next));
    auditEvent("identity.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Identity {
    const current = this.repository.require(tenantId, id);
    if (isIdentityTerminal(current)) {
      throw new IllegalTransitionError(`identity ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`identity ${id} cannot fall below zero`);
    }
    return this.repository.save(withIdentityAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Identity {
    return this.repository.save(withIdentityLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyIdentityTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("identity.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Identity[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): IdentityPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): IdentitySummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: identityStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isIdentityTerminal(current)) {
      throw new IllegalTransitionError(`identity ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("identity.discarded", { tenantId, id });
  }
}
