import {
  Task,
  TaskStatus,
  applyTaskTransition,
  canTaskTransition,
  isTaskTerminal,
  newTask,
  withTaskAmount,
  withTaskLabel,
  taskStatusCounts,
} from "./model";
import { TaskPage, TaskRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface TaskSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<TaskStatus, number>;
}

/** Business rules for the workflow task lifecycle. */
export class TaskService {
  constructor(private readonly repository: TaskRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Task {
    const draft = withTaskAmount(newTask(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("task.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Task {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: TaskStatus): Task {
    const current = this.repository.require(tenantId, id);
    if (isTaskTerminal(current)) {
      throw new IllegalTransitionError(`task ${id} is terminal`);
    }
    if (!canTaskTransition(current.status, next)) {
      throw new IllegalTransitionError(`task ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyTaskTransition(current, next));
    auditEvent("task.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Task {
    const current = this.repository.require(tenantId, id);
    if (isTaskTerminal(current)) {
      throw new IllegalTransitionError(`task ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`task ${id} cannot fall below zero`);
    }
    return this.repository.save(withTaskAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Task {
    return this.repository.save(withTaskLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyTaskTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("task.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Task[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): TaskPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): TaskSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: taskStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isTaskTerminal(current)) {
      throw new IllegalTransitionError(`task ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("task.discarded", { tenantId, id });
  }
}
