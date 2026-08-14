import {
  Schedule,
  ScheduleStatus,
  applyScheduleTransition,
  canScheduleTransition,
  isScheduleTerminal,
  newSchedule,
  withScheduleAmount,
  withScheduleLabel,
  scheduleStatusCounts,
} from "./model";
import { SchedulePage, ScheduleRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface ScheduleSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<ScheduleStatus, number>;
}

/** Business rules for the scheduled run lifecycle. */
export class ScheduleService {
  constructor(private readonly repository: ScheduleRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Schedule {
    const draft = withScheduleAmount(newSchedule(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("schedule.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Schedule {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: ScheduleStatus): Schedule {
    const current = this.repository.require(tenantId, id);
    if (isScheduleTerminal(current)) {
      throw new IllegalTransitionError(`schedule ${id} is terminal`);
    }
    if (!canScheduleTransition(current.status, next)) {
      throw new IllegalTransitionError(`schedule ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyScheduleTransition(current, next));
    auditEvent("schedule.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Schedule {
    const current = this.repository.require(tenantId, id);
    if (isScheduleTerminal(current)) {
      throw new IllegalTransitionError(`schedule ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`schedule ${id} cannot fall below zero`);
    }
    return this.repository.save(withScheduleAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Schedule {
    return this.repository.save(withScheduleLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyScheduleTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("schedule.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Schedule[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): SchedulePage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): ScheduleSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: scheduleStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isScheduleTerminal(current)) {
      throw new IllegalTransitionError(`schedule ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("schedule.discarded", { tenantId, id });
  }
}
