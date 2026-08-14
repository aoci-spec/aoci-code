import {
  ImportRun,
  ImportRunStatus,
  applyImportRunTransition,
  canImportRunTransition,
  isImportRunTerminal,
  newImportRun,
  withImportRunAmount,
  withImportRunLabel,
  importStatusCounts,
} from "./model";
import { ImportRunPage, ImportRunRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface ImportRunSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<ImportRunStatus, number>;
}

/** Business rules for the bulk import run lifecycle. */
export class ImportRunService {
  constructor(private readonly repository: ImportRunRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): ImportRun {
    const draft = withImportRunAmount(newImportRun(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("import.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): ImportRun {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: ImportRunStatus): ImportRun {
    const current = this.repository.require(tenantId, id);
    if (isImportRunTerminal(current)) {
      throw new IllegalTransitionError(`import ${id} is terminal`);
    }
    if (!canImportRunTransition(current.status, next)) {
      throw new IllegalTransitionError(`import ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyImportRunTransition(current, next));
    auditEvent("import.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): ImportRun {
    const current = this.repository.require(tenantId, id);
    if (isImportRunTerminal(current)) {
      throw new IllegalTransitionError(`import ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`import ${id} cannot fall below zero`);
    }
    return this.repository.save(withImportRunAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): ImportRun {
    return this.repository.save(withImportRunLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyImportRunTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("import.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): ImportRun[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): ImportRunPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): ImportRunSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: importStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isImportRunTerminal(current)) {
      throw new IllegalTransitionError(`import ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("import.discarded", { tenantId, id });
  }
}
