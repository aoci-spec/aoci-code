import { NextFunction, Request, Response } from "express";
import { CouponService } from "./service";
import {
  parseCouponCreate,
  parseCouponLabel,
  parseCouponPage,
  parseCouponTransition,
} from "./validator";
import { toCouponPagePayload, toCouponPayload, toCouponSummaryPayload } from "./mapper";
import { assertCouponAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for coupon grant resources. */
export function makeCouponHandlers(service: CouponService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCouponAccess(tenantId, "write");
        const input = parseCouponCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toCouponPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCouponAccess(tenantId, "read");
        response.json(toCouponPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCouponAccess(tenantId, "write");
        const input = parseCouponTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toCouponPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCouponAccess(tenantId, "write");
        const input = parseCouponLabel(request.body);
        response.json(toCouponPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCouponAccess(tenantId, "read");
        const page = parseCouponPage(request.query);
        response.json(toCouponPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCouponAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toCouponPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCouponAccess(tenantId, "read");
        response.json(toCouponSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCouponAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
