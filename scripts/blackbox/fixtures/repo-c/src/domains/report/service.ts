import {
  Report,
  ReportStatus,
  applyReportTransition,
  canReportTransition,
  isReportTerminal,
  newReport,
  withReportAmount,
  withReportLabel,
  reportStatusCounts,
} from "./model";
import { ReportPage, ReportRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface ReportSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<ReportStatus, number>;
}

/** Business rules for the generated report lifecycle. */
export class ReportService {
  constructor(private readonly repository: ReportRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Report {
    const draft = withReportAmount(newReport(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("report.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Report {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: ReportStatus): Report {
    const current = this.repository.require(tenantId, id);
    if (isReportTerminal(current)) {
      throw new IllegalTransitionError(`report ${id} is terminal`);
    }
    if (!canReportTransition(current.status, next)) {
      throw new IllegalTransitionError(`report ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyReportTransition(current, next));
    auditEvent("report.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Report {
    const current = this.repository.require(tenantId, id);
    if (isReportTerminal(current)) {
      throw new IllegalTransitionError(`report ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`report ${id} cannot fall below zero`);
    }
    return this.repository.save(withReportAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Report {
    return this.repository.save(withReportLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyReportTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("report.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Report[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): ReportPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): ReportSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: reportStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isReportTerminal(current)) {
      throw new IllegalTransitionError(`report ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("report.discarded", { tenantId, id });
  }
}
