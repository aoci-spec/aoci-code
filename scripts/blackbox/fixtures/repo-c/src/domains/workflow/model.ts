import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one workflow instance. */
export interface Workflow {
  readonly id: string;
  readonly tenantId: string;
  status: WorkflowStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly WorkflowChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface WorkflowChange {
  readonly at: string;
  readonly from: WorkflowStatus;
  readonly to: WorkflowStatus;
}

export type WorkflowStatus = "draft" | "active" | "settled" | "cancelled";

export const workflowStatuses: readonly WorkflowStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a workflow instance; anything else is rejected upstream. */
const transitions: Record<WorkflowStatus, readonly WorkflowStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canWorkflowTransition(from: WorkflowStatus, to: WorkflowStatus): boolean {
  return transitions[from].includes(to);
}

export function isWorkflowTerminal(value: Workflow): boolean {
  return transitions[value.status].length === 0;
}

export function newWorkflow(id: string, tenantId: string, reference: string): Workflow {
  const now = isoTimestamp();
  return {
    id,
    tenantId,
    status: "draft",
    amountCents: 0,
    reference,
    labels: [],
    history: [],
    createdAt: now,
    updatedAt: now,
  };
}

export function touchWorkflow(value: Workflow): Workflow {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyWorkflowTransition(value: Workflow, to: WorkflowStatus): Workflow {
  const change: WorkflowChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withWorkflowAmount(value: Workflow, amountCents: number): Workflow {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("workflow amount must be a non-negative integer");
  }
  return touchWorkflow({ ...value, amountCents });
}

export function withWorkflowLabel(value: Workflow, label: string): Workflow {
  if (label.trim().length === 0) {
    throw new ValidationError("workflow label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchWorkflow({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutWorkflowLabel(value: Workflow, label: string): Workflow {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchWorkflow({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateWorkflow(value: Workflow): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("workflow requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("workflow reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("workflow amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("workflow updatedAt precedes createdAt");
  }
}

export function compareWorkflow(left: Workflow, right: Workflow): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeWorkflow(value: Workflow): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function workflowStatusCounts(values: readonly Workflow[]): Record<WorkflowStatus, number> {
  const counts: Record<WorkflowStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
