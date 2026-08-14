import {
  Product,
  ProductStatus,
  applyProductTransition,
  canProductTransition,
  isProductTerminal,
  newProduct,
  withProductAmount,
  withProductLabel,
  productStatusCounts,
} from "./model";
import { ProductPage, ProductRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface ProductSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<ProductStatus, number>;
}

/** Business rules for the sellable product lifecycle. */
export class ProductService {
  constructor(private readonly repository: ProductRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Product {
    const draft = withProductAmount(newProduct(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("product.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Product {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: ProductStatus): Product {
    const current = this.repository.require(tenantId, id);
    if (isProductTerminal(current)) {
      throw new IllegalTransitionError(`product ${id} is terminal`);
    }
    if (!canProductTransition(current.status, next)) {
      throw new IllegalTransitionError(`product ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyProductTransition(current, next));
    auditEvent("product.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Product {
    const current = this.repository.require(tenantId, id);
    if (isProductTerminal(current)) {
      throw new IllegalTransitionError(`product ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`product ${id} cannot fall below zero`);
    }
    return this.repository.save(withProductAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Product {
    return this.repository.save(withProductLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyProductTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("product.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Product[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): ProductPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): ProductSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: productStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isProductTerminal(current)) {
      throw new IllegalTransitionError(`product ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("product.discarded", { tenantId, id });
  }
}
