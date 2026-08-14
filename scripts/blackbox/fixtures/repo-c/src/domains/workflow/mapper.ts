import { Workflow, WorkflowStatus, summarizeWorkflow } from "./model";
import { WorkflowPage } from "./repository";
import { WorkflowSummary } from "./service";

export interface WorkflowPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface WorkflowPagePayload {
  items: readonly WorkflowPayload[];
  total: number;
  offset: number;
}

export interface WorkflowSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<WorkflowStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a workflow instance; tenant identity never leaves the service. */
export function toWorkflowPayload(value: Workflow): WorkflowPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeWorkflow(value),
    updatedAt: value.updatedAt,
  };
}

export function toWorkflowPayloads(values: readonly Workflow[]): WorkflowPayload[] {
  return values.map(toWorkflowPayload);
}

export function toWorkflowPagePayload(page: WorkflowPage): WorkflowPagePayload {
  return { items: toWorkflowPayloads(page.items), total: page.total, offset: page.offset };
}

export function toWorkflowSummaryPayload(summary: WorkflowSummary): WorkflowSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Workflow[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toWorkflowCsvRow(value: Workflow): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
