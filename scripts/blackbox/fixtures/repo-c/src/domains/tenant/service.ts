import {
  Tenant,
  TenantStatus,
  applyTenantTransition,
  canTenantTransition,
  isTenantTerminal,
  newTenant,
  withTenantAmount,
  withTenantLabel,
  tenantStatusCounts,
} from "./model";
import { TenantPage, TenantRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface TenantSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<TenantStatus, number>;
}

/** Business rules for the tenant boundary lifecycle. */
export class TenantService {
  constructor(private readonly repository: TenantRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Tenant {
    const draft = withTenantAmount(newTenant(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("tenant.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Tenant {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: TenantStatus): Tenant {
    const current = this.repository.require(tenantId, id);
    if (isTenantTerminal(current)) {
      throw new IllegalTransitionError(`tenant ${id} is terminal`);
    }
    if (!canTenantTransition(current.status, next)) {
      throw new IllegalTransitionError(`tenant ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyTenantTransition(current, next));
    auditEvent("tenant.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Tenant {
    const current = this.repository.require(tenantId, id);
    if (isTenantTerminal(current)) {
      throw new IllegalTransitionError(`tenant ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`tenant ${id} cannot fall below zero`);
    }
    return this.repository.save(withTenantAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Tenant {
    return this.repository.save(withTenantLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyTenantTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("tenant.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Tenant[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): TenantPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): TenantSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: tenantStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isTenantTerminal(current)) {
      throw new IllegalTransitionError(`tenant ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("tenant.discarded", { tenantId, id });
  }
}
