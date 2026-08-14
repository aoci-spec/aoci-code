import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type PlanAction = "read" | "write" | "administer";

const required: Record<PlanAction, readonly string[]> = {
  read: ["plan:read"],
  write: ["plan:read", "plan:write"],
  administer: ["plan:read", "plan:write", "plan:admin"],
};

/** Tenant-scoped authorization for subscription plan operations. */
export function assertPlanAccess(tenantId: string, action: PlanAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("plan access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`plan ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayPlan(tenantId: string, action: PlanAction): boolean {
  try {
    assertPlanAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): PlanAction[] {
  const actions: PlanAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayPlan(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("plan belongs to a different tenant");
  }
}
