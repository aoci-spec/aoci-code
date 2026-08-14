import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type WorkflowAction = "read" | "write" | "administer";

const required: Record<WorkflowAction, readonly string[]> = {
  read: ["workflow:read"],
  write: ["workflow:read", "workflow:write"],
  administer: ["workflow:read", "workflow:write", "workflow:admin"],
};

/** Tenant-scoped authorization for workflow instance operations. */
export function assertWorkflowAccess(tenantId: string, action: WorkflowAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("workflow access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`workflow ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayWorkflow(tenantId: string, action: WorkflowAction): boolean {
  try {
    assertWorkflowAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): WorkflowAction[] {
  const actions: WorkflowAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayWorkflow(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("workflow belongs to a different tenant");
  }
}
