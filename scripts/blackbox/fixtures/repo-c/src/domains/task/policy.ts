import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type TaskAction = "read" | "write" | "administer";

const required: Record<TaskAction, readonly string[]> = {
  read: ["task:read"],
  write: ["task:read", "task:write"],
  administer: ["task:read", "task:write", "task:admin"],
};

/** Tenant-scoped authorization for workflow task operations. */
export function assertTaskAccess(tenantId: string, action: TaskAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("task access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`task ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayTask(tenantId: string, action: TaskAction): boolean {
  try {
    assertTaskAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): TaskAction[] {
  const actions: TaskAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayTask(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("task belongs to a different tenant");
  }
}
