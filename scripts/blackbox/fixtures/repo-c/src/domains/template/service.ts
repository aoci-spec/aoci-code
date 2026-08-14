import {
  Template,
  TemplateStatus,
  applyTemplateTransition,
  canTemplateTransition,
  isTemplateTerminal,
  newTemplate,
  withTemplateAmount,
  withTemplateLabel,
  templateStatusCounts,
} from "./model";
import { TemplatePage, TemplateRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface TemplateSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<TemplateStatus, number>;
}

/** Business rules for the message template lifecycle. */
export class TemplateService {
  constructor(private readonly repository: TemplateRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Template {
    const draft = withTemplateAmount(newTemplate(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("template.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Template {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: TemplateStatus): Template {
    const current = this.repository.require(tenantId, id);
    if (isTemplateTerminal(current)) {
      throw new IllegalTransitionError(`template ${id} is terminal`);
    }
    if (!canTemplateTransition(current.status, next)) {
      throw new IllegalTransitionError(`template ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyTemplateTransition(current, next));
    auditEvent("template.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Template {
    const current = this.repository.require(tenantId, id);
    if (isTemplateTerminal(current)) {
      throw new IllegalTransitionError(`template ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`template ${id} cannot fall below zero`);
    }
    return this.repository.save(withTemplateAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Template {
    return this.repository.save(withTemplateLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyTemplateTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("template.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Template[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): TemplatePage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): TemplateSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: templateStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isTemplateTerminal(current)) {
      throw new IllegalTransitionError(`template ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("template.discarded", { tenantId, id });
  }
}
