import {
  Job,
  JobStatus,
  applyJobTransition,
  canJobTransition,
  isJobTerminal,
  newJob,
  withJobAmount,
  withJobLabel,
  jobStatusCounts,
} from "./model";
import { JobPage, JobRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface JobSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<JobStatus, number>;
}

/** Business rules for the background job lifecycle. */
export class JobService {
  constructor(private readonly repository: JobRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Job {
    const draft = withJobAmount(newJob(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("job.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Job {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: JobStatus): Job {
    const current = this.repository.require(tenantId, id);
    if (isJobTerminal(current)) {
      throw new IllegalTransitionError(`job ${id} is terminal`);
    }
    if (!canJobTransition(current.status, next)) {
      throw new IllegalTransitionError(`job ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyJobTransition(current, next));
    auditEvent("job.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Job {
    const current = this.repository.require(tenantId, id);
    if (isJobTerminal(current)) {
      throw new IllegalTransitionError(`job ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`job ${id} cannot fall below zero`);
    }
    return this.repository.save(withJobAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Job {
    return this.repository.save(withJobLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyJobTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("job.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Job[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): JobPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): JobSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: jobStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isJobTerminal(current)) {
      throw new IllegalTransitionError(`job ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("job.discarded", { tenantId, id });
  }
}
