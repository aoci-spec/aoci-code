import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type JobAction = "read" | "write" | "administer";

const required: Record<JobAction, readonly string[]> = {
  read: ["job:read"],
  write: ["job:read", "job:write"],
  administer: ["job:read", "job:write", "job:admin"],
};

/** Tenant-scoped authorization for background job operations. */
export function assertJobAccess(tenantId: string, action: JobAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("job access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`job ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayJob(tenantId: string, action: JobAction): boolean {
  try {
    assertJobAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): JobAction[] {
  const actions: JobAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayJob(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("job belongs to a different tenant");
  }
}
