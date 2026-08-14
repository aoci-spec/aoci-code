import {
  Variant,
  VariantStatus,
  applyVariantTransition,
  canVariantTransition,
  isVariantTerminal,
  newVariant,
  withVariantAmount,
  withVariantLabel,
  variantStatusCounts,
} from "./model";
import { VariantPage, VariantRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface VariantSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<VariantStatus, number>;
}

/** Business rules for the product variant lifecycle. */
export class VariantService {
  constructor(private readonly repository: VariantRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Variant {
    const draft = withVariantAmount(newVariant(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("variant.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Variant {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: VariantStatus): Variant {
    const current = this.repository.require(tenantId, id);
    if (isVariantTerminal(current)) {
      throw new IllegalTransitionError(`variant ${id} is terminal`);
    }
    if (!canVariantTransition(current.status, next)) {
      throw new IllegalTransitionError(`variant ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyVariantTransition(current, next));
    auditEvent("variant.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Variant {
    const current = this.repository.require(tenantId, id);
    if (isVariantTerminal(current)) {
      throw new IllegalTransitionError(`variant ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`variant ${id} cannot fall below zero`);
    }
    return this.repository.save(withVariantAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Variant {
    return this.repository.save(withVariantLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyVariantTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("variant.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Variant[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): VariantPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): VariantSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: variantStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isVariantTerminal(current)) {
      throw new IllegalTransitionError(`variant ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("variant.discarded", { tenantId, id });
  }
}
