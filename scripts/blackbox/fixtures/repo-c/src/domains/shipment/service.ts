import {
  Shipment,
  ShipmentStatus,
  applyShipmentTransition,
  canShipmentTransition,
  isShipmentTerminal,
  newShipment,
  withShipmentAmount,
  withShipmentLabel,
  shipmentStatusCounts,
} from "./model";
import { ShipmentPage, ShipmentRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface ShipmentSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<ShipmentStatus, number>;
}

/** Business rules for the outbound shipment lifecycle. */
export class ShipmentService {
  constructor(private readonly repository: ShipmentRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Shipment {
    const draft = withShipmentAmount(newShipment(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("shipment.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Shipment {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: ShipmentStatus): Shipment {
    const current = this.repository.require(tenantId, id);
    if (isShipmentTerminal(current)) {
      throw new IllegalTransitionError(`shipment ${id} is terminal`);
    }
    if (!canShipmentTransition(current.status, next)) {
      throw new IllegalTransitionError(`shipment ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyShipmentTransition(current, next));
    auditEvent("shipment.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Shipment {
    const current = this.repository.require(tenantId, id);
    if (isShipmentTerminal(current)) {
      throw new IllegalTransitionError(`shipment ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`shipment ${id} cannot fall below zero`);
    }
    return this.repository.save(withShipmentAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Shipment {
    return this.repository.save(withShipmentLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyShipmentTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("shipment.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Shipment[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): ShipmentPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): ShipmentSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: shipmentStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isShipmentTerminal(current)) {
      throw new IllegalTransitionError(`shipment ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("shipment.discarded", { tenantId, id });
  }
}
