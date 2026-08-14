import {
  Inventory,
  InventoryStatus,
  applyInventoryTransition,
  canInventoryTransition,
  isInventoryTerminal,
  newInventory,
  withInventoryAmount,
  withInventoryLabel,
  inventoryStatusCounts,
} from "./model";
import { InventoryPage, InventoryRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface InventorySummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<InventoryStatus, number>;
}

/** Business rules for the stock position lifecycle. */
export class InventoryService {
  constructor(private readonly repository: InventoryRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Inventory {
    const draft = withInventoryAmount(newInventory(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("inventory.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Inventory {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: InventoryStatus): Inventory {
    const current = this.repository.require(tenantId, id);
    if (isInventoryTerminal(current)) {
      throw new IllegalTransitionError(`inventory ${id} is terminal`);
    }
    if (!canInventoryTransition(current.status, next)) {
      throw new IllegalTransitionError(`inventory ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyInventoryTransition(current, next));
    auditEvent("inventory.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Inventory {
    const current = this.repository.require(tenantId, id);
    if (isInventoryTerminal(current)) {
      throw new IllegalTransitionError(`inventory ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`inventory ${id} cannot fall below zero`);
    }
    return this.repository.save(withInventoryAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Inventory {
    return this.repository.save(withInventoryLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyInventoryTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("inventory.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Inventory[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): InventoryPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): InventorySummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: inventoryStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isInventoryTerminal(current)) {
      throw new IllegalTransitionError(`inventory ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("inventory.discarded", { tenantId, id });
  }
}
