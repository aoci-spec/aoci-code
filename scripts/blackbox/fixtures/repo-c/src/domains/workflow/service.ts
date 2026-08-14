import {
  Workflow,
  WorkflowStatus,
  applyWorkflowTransition,
  canWorkflowTransition,
  isWorkflowTerminal,
  newWorkflow,
  withWorkflowAmount,
  withWorkflowLabel,
  workflowStatusCounts,
} from "./model";
import { WorkflowPage, WorkflowRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface WorkflowSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<WorkflowStatus, number>;
}

/** Business rules for the workflow instance lifecycle. */
export class WorkflowService {
  constructor(private readonly repository: WorkflowRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Workflow {
    const draft = withWorkflowAmount(newWorkflow(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("workflow.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Workflow {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: WorkflowStatus): Workflow {
    const current = this.repository.require(tenantId, id);
    if (isWorkflowTerminal(current)) {
      throw new IllegalTransitionError(`workflow ${id} is terminal`);
    }
    if (!canWorkflowTransition(current.status, next)) {
      throw new IllegalTransitionError(`workflow ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyWorkflowTransition(current, next));
    auditEvent("workflow.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Workflow {
    const current = this.repository.require(tenantId, id);
    if (isWorkflowTerminal(current)) {
      throw new IllegalTransitionError(`workflow ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`workflow ${id} cannot fall below zero`);
    }
    return this.repository.save(withWorkflowAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Workflow {
    return this.repository.save(withWorkflowLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyWorkflowTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("workflow.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Workflow[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): WorkflowPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): WorkflowSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: workflowStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isWorkflowTerminal(current)) {
      throw new IllegalTransitionError(`workflow ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("workflow.discarded", { tenantId, id });
  }
}
