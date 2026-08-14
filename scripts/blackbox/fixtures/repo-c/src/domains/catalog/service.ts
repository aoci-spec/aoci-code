import {
  Catalog,
  CatalogStatus,
  applyCatalogTransition,
  canCatalogTransition,
  isCatalogTerminal,
  newCatalog,
  withCatalogAmount,
  withCatalogLabel,
  catalogStatusCounts,
} from "./model";
import { CatalogPage, CatalogRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface CatalogSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<CatalogStatus, number>;
}

/** Business rules for the product catalog lifecycle. */
export class CatalogService {
  constructor(private readonly repository: CatalogRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Catalog {
    const draft = withCatalogAmount(newCatalog(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("catalog.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Catalog {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: CatalogStatus): Catalog {
    const current = this.repository.require(tenantId, id);
    if (isCatalogTerminal(current)) {
      throw new IllegalTransitionError(`catalog ${id} is terminal`);
    }
    if (!canCatalogTransition(current.status, next)) {
      throw new IllegalTransitionError(`catalog ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyCatalogTransition(current, next));
    auditEvent("catalog.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Catalog {
    const current = this.repository.require(tenantId, id);
    if (isCatalogTerminal(current)) {
      throw new IllegalTransitionError(`catalog ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`catalog ${id} cannot fall below zero`);
    }
    return this.repository.save(withCatalogAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Catalog {
    return this.repository.save(withCatalogLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyCatalogTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("catalog.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Catalog[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): CatalogPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): CatalogSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: catalogStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isCatalogTerminal(current)) {
      throw new IllegalTransitionError(`catalog ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("catalog.discarded", { tenantId, id });
  }
}
