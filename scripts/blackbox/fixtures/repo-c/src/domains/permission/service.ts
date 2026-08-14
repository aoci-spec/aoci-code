import {
  Permission,
  PermissionStatus,
  applyPermissionTransition,
  canPermissionTransition,
  isPermissionTerminal,
  newPermission,
  withPermissionAmount,
  withPermissionLabel,
  permissionStatusCounts,
} from "./model";
import { PermissionPage, PermissionRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface PermissionSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<PermissionStatus, number>;
}

/** Business rules for the permission grant lifecycle. */
export class PermissionService {
  constructor(private readonly repository: PermissionRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Permission {
    const draft = withPermissionAmount(newPermission(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("permission.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Permission {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: PermissionStatus): Permission {
    const current = this.repository.require(tenantId, id);
    if (isPermissionTerminal(current)) {
      throw new IllegalTransitionError(`permission ${id} is terminal`);
    }
    if (!canPermissionTransition(current.status, next)) {
      throw new IllegalTransitionError(`permission ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyPermissionTransition(current, next));
    auditEvent("permission.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Permission {
    const current = this.repository.require(tenantId, id);
    if (isPermissionTerminal(current)) {
      throw new IllegalTransitionError(`permission ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`permission ${id} cannot fall below zero`);
    }
    return this.repository.save(withPermissionAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Permission {
    return this.repository.save(withPermissionLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyPermissionTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("permission.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Permission[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): PermissionPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): PermissionSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: permissionStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isPermissionTerminal(current)) {
      throw new IllegalTransitionError(`permission ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("permission.discarded", { tenantId, id });
  }
}
