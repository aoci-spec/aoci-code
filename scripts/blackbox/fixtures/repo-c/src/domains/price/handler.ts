import { NextFunction, Request, Response } from "express";
import { PriceService } from "./service";
import {
  parsePriceCreate,
  parsePriceLabel,
  parsePricePage,
  parsePriceTransition,
} from "./validator";
import { toPricePagePayload, toPricePayload, toPriceSummaryPayload } from "./mapper";
import { assertPriceAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for price definition resources. */
export function makePriceHandlers(service: PriceService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPriceAccess(tenantId, "write");
        const input = parsePriceCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toPricePayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPriceAccess(tenantId, "read");
        response.json(toPricePayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPriceAccess(tenantId, "write");
        const input = parsePriceTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toPricePayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPriceAccess(tenantId, "write");
        const input = parsePriceLabel(request.body);
        response.json(toPricePayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPriceAccess(tenantId, "read");
        const page = parsePricePage(request.query);
        response.json(toPricePagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPriceAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toPricePayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPriceAccess(tenantId, "read");
        response.json(toPriceSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPriceAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
