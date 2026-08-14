import {
  Warehouse,
  WarehouseStatus,
  applyWarehouseTransition,
  canWarehouseTransition,
  isWarehouseTerminal,
  newWarehouse,
  withWarehouseAmount,
  withWarehouseLabel,
  warehouseStatusCounts,
} from "./model";
import { WarehousePage, WarehouseRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface WarehouseSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<WarehouseStatus, number>;
}

/** Business rules for the storage facility lifecycle. */
export class WarehouseService {
  constructor(private readonly repository: WarehouseRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Warehouse {
    const draft = withWarehouseAmount(newWarehouse(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("warehouse.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Warehouse {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: WarehouseStatus): Warehouse {
    const current = this.repository.require(tenantId, id);
    if (isWarehouseTerminal(current)) {
      throw new IllegalTransitionError(`warehouse ${id} is terminal`);
    }
    if (!canWarehouseTransition(current.status, next)) {
      throw new IllegalTransitionError(`warehouse ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyWarehouseTransition(current, next));
    auditEvent("warehouse.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Warehouse {
    const current = this.repository.require(tenantId, id);
    if (isWarehouseTerminal(current)) {
      throw new IllegalTransitionError(`warehouse ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`warehouse ${id} cannot fall below zero`);
    }
    return this.repository.save(withWarehouseAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Warehouse {
    return this.repository.save(withWarehouseLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyWarehouseTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("warehouse.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Warehouse[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): WarehousePage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): WarehouseSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: warehouseStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isWarehouseTerminal(current)) {
      throw new IllegalTransitionError(`warehouse ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("warehouse.discarded", { tenantId, id });
  }
}
