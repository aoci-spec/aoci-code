import {
  ExportRun,
  ExportRunStatus,
  applyExportRunTransition,
  canExportRunTransition,
  isExportRunTerminal,
  newExportRun,
  withExportRunAmount,
  withExportRunLabel,
  exportStatusCounts,
} from "./model";
import { ExportRunPage, ExportRunRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface ExportRunSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<ExportRunStatus, number>;
}

/** Business rules for the bulk export run lifecycle. */
export class ExportRunService {
  constructor(private readonly repository: ExportRunRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): ExportRun {
    const draft = withExportRunAmount(newExportRun(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("export.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): ExportRun {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: ExportRunStatus): ExportRun {
    const current = this.repository.require(tenantId, id);
    if (isExportRunTerminal(current)) {
      throw new IllegalTransitionError(`export ${id} is terminal`);
    }
    if (!canExportRunTransition(current.status, next)) {
      throw new IllegalTransitionError(`export ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyExportRunTransition(current, next));
    auditEvent("export.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): ExportRun {
    const current = this.repository.require(tenantId, id);
    if (isExportRunTerminal(current)) {
      throw new IllegalTransitionError(`export ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`export ${id} cannot fall below zero`);
    }
    return this.repository.save(withExportRunAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): ExportRun {
    return this.repository.save(withExportRunLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyExportRunTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("export.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): ExportRun[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): ExportRunPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): ExportRunSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: exportStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isExportRunTerminal(current)) {
      throw new IllegalTransitionError(`export ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("export.discarded", { tenantId, id });
  }
}
