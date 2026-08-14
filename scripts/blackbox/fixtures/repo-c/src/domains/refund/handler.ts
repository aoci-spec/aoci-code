import { NextFunction, Request, Response } from "express";
import { RefundService } from "./service";
import {
  parseRefundCreate,
  parseRefundLabel,
  parseRefundPage,
  parseRefundTransition,
} from "./validator";
import { toRefundPagePayload, toRefundPayload, toRefundSummaryPayload } from "./mapper";
import { assertRefundAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for refund request resources. */
export function makeRefundHandlers(service: RefundService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRefundAccess(tenantId, "write");
        const input = parseRefundCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toRefundPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRefundAccess(tenantId, "read");
        response.json(toRefundPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRefundAccess(tenantId, "write");
        const input = parseRefundTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toRefundPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRefundAccess(tenantId, "write");
        const input = parseRefundLabel(request.body);
        response.json(toRefundPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRefundAccess(tenantId, "read");
        const page = parseRefundPage(request.query);
        response.json(toRefundPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRefundAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toRefundPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRefundAccess(tenantId, "read");
        response.json(toRefundSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRefundAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
