import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type ScheduleAction = "read" | "write" | "administer";

const required: Record<ScheduleAction, readonly string[]> = {
  read: ["schedule:read"],
  write: ["schedule:read", "schedule:write"],
  administer: ["schedule:read", "schedule:write", "schedule:admin"],
};

/** Tenant-scoped authorization for scheduled run operations. */
export function assertScheduleAccess(tenantId: string, action: ScheduleAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("schedule access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`schedule ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function maySchedule(tenantId: string, action: ScheduleAction): boolean {
  try {
    assertScheduleAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): ScheduleAction[] {
  const actions: ScheduleAction[] = ["read", "write", "administer"];
  return actions.filter((action) => maySchedule(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("schedule belongs to a different tenant");
  }
}
