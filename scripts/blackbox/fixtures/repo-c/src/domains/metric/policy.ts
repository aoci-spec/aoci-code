import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type MetricAction = "read" | "write" | "administer";

const required: Record<MetricAction, readonly string[]> = {
  read: ["metric:read"],
  write: ["metric:read", "metric:write"],
  administer: ["metric:read", "metric:write", "metric:admin"],
};

/** Tenant-scoped authorization for aggregated metric operations. */
export function assertMetricAccess(tenantId: string, action: MetricAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("metric access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`metric ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayMetric(tenantId: string, action: MetricAction): boolean {
  try {
    assertMetricAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): MetricAction[] {
  const actions: MetricAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayMetric(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("metric belongs to a different tenant");
  }
}
