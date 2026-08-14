import { NextFunction, Request, Response } from "express";
import { DiscountService } from "./service";
import {
  parseDiscountCreate,
  parseDiscountLabel,
  parseDiscountPage,
  parseDiscountTransition,
} from "./validator";
import { toDiscountPagePayload, toDiscountPayload, toDiscountSummaryPayload } from "./mapper";
import { assertDiscountAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for discount rule resources. */
export function makeDiscountHandlers(service: DiscountService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDiscountAccess(tenantId, "write");
        const input = parseDiscountCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toDiscountPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDiscountAccess(tenantId, "read");
        response.json(toDiscountPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDiscountAccess(tenantId, "write");
        const input = parseDiscountTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toDiscountPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDiscountAccess(tenantId, "write");
        const input = parseDiscountLabel(request.body);
        response.json(toDiscountPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDiscountAccess(tenantId, "read");
        const page = parseDiscountPage(request.query);
        response.json(toDiscountPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDiscountAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toDiscountPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDiscountAccess(tenantId, "read");
        response.json(toDiscountSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDiscountAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
