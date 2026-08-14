import {
  Warranty,
  WarrantyStatus,
  applyWarrantyTransition,
  canWarrantyTransition,
  isWarrantyTerminal,
  newWarranty,
  withWarrantyAmount,
  withWarrantyLabel,
  warrantyStatusCounts,
} from "./model";
import { WarrantyPage, WarrantyRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface WarrantySummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<WarrantyStatus, number>;
}

/** Business rules for the warranty claim lifecycle. */
export class WarrantyService {
  constructor(private readonly repository: WarrantyRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Warranty {
    const draft = withWarrantyAmount(newWarranty(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("warranty.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Warranty {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: WarrantyStatus): Warranty {
    const current = this.repository.require(tenantId, id);
    if (isWarrantyTerminal(current)) {
      throw new IllegalTransitionError(`warranty ${id} is terminal`);
    }
    if (!canWarrantyTransition(current.status, next)) {
      throw new IllegalTransitionError(`warranty ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyWarrantyTransition(current, next));
    auditEvent("warranty.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Warranty {
    const current = this.repository.require(tenantId, id);
    if (isWarrantyTerminal(current)) {
      throw new IllegalTransitionError(`warranty ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`warranty ${id} cannot fall below zero`);
    }
    return this.repository.save(withWarrantyAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Warranty {
    return this.repository.save(withWarrantyLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyWarrantyTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("warranty.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Warranty[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): WarrantyPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): WarrantySummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: warrantyStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isWarrantyTerminal(current)) {
      throw new IllegalTransitionError(`warranty ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("warranty.discarded", { tenantId, id });
  }
}
