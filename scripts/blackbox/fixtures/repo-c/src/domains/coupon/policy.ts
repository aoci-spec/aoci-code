import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type CouponAction = "read" | "write" | "administer";

const required: Record<CouponAction, readonly string[]> = {
  read: ["coupon:read"],
  write: ["coupon:read", "coupon:write"],
  administer: ["coupon:read", "coupon:write", "coupon:admin"],
};

/** Tenant-scoped authorization for coupon grant operations. */
export function assertCouponAccess(tenantId: string, action: CouponAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("coupon access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`coupon ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayCoupon(tenantId: string, action: CouponAction): boolean {
  try {
    assertCouponAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): CouponAction[] {
  const actions: CouponAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayCoupon(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("coupon belongs to a different tenant");
  }
}
