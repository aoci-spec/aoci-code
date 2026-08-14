import {
  Order,
  OrderStatus,
  applyOrderTransition,
  canOrderTransition,
  isOrderTerminal,
  newOrder,
  withOrderAmount,
  withOrderLabel,
  orderStatusCounts,
} from "./model";
import { OrderPage, OrderRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface OrderSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<OrderStatus, number>;
}

/** Business rules for the purchase order lifecycle. */
export class OrderService {
  constructor(private readonly repository: OrderRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Order {
    const draft = withOrderAmount(newOrder(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("order.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Order {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: OrderStatus): Order {
    const current = this.repository.require(tenantId, id);
    if (isOrderTerminal(current)) {
      throw new IllegalTransitionError(`order ${id} is terminal`);
    }
    if (!canOrderTransition(current.status, next)) {
      throw new IllegalTransitionError(`order ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyOrderTransition(current, next));
    auditEvent("order.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Order {
    const current = this.repository.require(tenantId, id);
    if (isOrderTerminal(current)) {
      throw new IllegalTransitionError(`order ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`order ${id} cannot fall below zero`);
    }
    return this.repository.save(withOrderAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Order {
    return this.repository.save(withOrderLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyOrderTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("order.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Order[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): OrderPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): OrderSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: orderStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isOrderTerminal(current)) {
      throw new IllegalTransitionError(`order ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("order.discarded", { tenantId, id });
  }
}
