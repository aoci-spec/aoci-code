import {
  Role,
  RoleStatus,
  applyRoleTransition,
  canRoleTransition,
  isRoleTerminal,
  newRole,
  withRoleAmount,
  withRoleLabel,
  roleStatusCounts,
} from "./model";
import { RolePage, RoleRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface RoleSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<RoleStatus, number>;
}

/** Business rules for the authorization role lifecycle. */
export class RoleService {
  constructor(private readonly repository: RoleRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Role {
    const draft = withRoleAmount(newRole(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("role.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Role {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: RoleStatus): Role {
    const current = this.repository.require(tenantId, id);
    if (isRoleTerminal(current)) {
      throw new IllegalTransitionError(`role ${id} is terminal`);
    }
    if (!canRoleTransition(current.status, next)) {
      throw new IllegalTransitionError(`role ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyRoleTransition(current, next));
    auditEvent("role.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Role {
    const current = this.repository.require(tenantId, id);
    if (isRoleTerminal(current)) {
      throw new IllegalTransitionError(`role ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`role ${id} cannot fall below zero`);
    }
    return this.repository.save(withRoleAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Role {
    return this.repository.save(withRoleLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyRoleTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("role.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Role[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): RolePage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): RoleSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: roleStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isRoleTerminal(current)) {
      throw new IllegalTransitionError(`role ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("role.discarded", { tenantId, id });
  }
}
