import { NextFunction, Request, Response } from "express";
import { OrderService } from "./service";
import {
  parseOrderCreate,
  parseOrderLabel,
  parseOrderPage,
  parseOrderTransition,
} from "./validator";
import { toOrderPagePayload, toOrderPayload, toOrderSummaryPayload } from "./mapper";
import { assertOrderAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for purchase order resources. */
export function makeOrderHandlers(service: OrderService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertOrderAccess(tenantId, "write");
        const input = parseOrderCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toOrderPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertOrderAccess(tenantId, "read");
        response.json(toOrderPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertOrderAccess(tenantId, "write");
        const input = parseOrderTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toOrderPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertOrderAccess(tenantId, "write");
        const input = parseOrderLabel(request.body);
        response.json(toOrderPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertOrderAccess(tenantId, "read");
        const page = parseOrderPage(request.query);
        response.json(toOrderPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertOrderAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toOrderPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertOrderAccess(tenantId, "read");
        response.json(toOrderSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertOrderAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
