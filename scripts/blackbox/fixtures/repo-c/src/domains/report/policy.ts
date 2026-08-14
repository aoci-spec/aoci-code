import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type ReportAction = "read" | "write" | "administer";

const required: Record<ReportAction, readonly string[]> = {
  read: ["report:read"],
  write: ["report:read", "report:write"],
  administer: ["report:read", "report:write", "report:admin"],
};

/** Tenant-scoped authorization for generated report operations. */
export function assertReportAccess(tenantId: string, action: ReportAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("report access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`report ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayReport(tenantId: string, action: ReportAction): boolean {
  try {
    assertReportAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): ReportAction[] {
  const actions: ReportAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayReport(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("report belongs to a different tenant");
  }
}
