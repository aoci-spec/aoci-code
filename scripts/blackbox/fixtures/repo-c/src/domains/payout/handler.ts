import { NextFunction, Request, Response } from "express";
import { PayoutService } from "./service";
import {
  parsePayoutCreate,
  parsePayoutLabel,
  parsePayoutPage,
  parsePayoutTransition,
} from "./validator";
import { toPayoutPagePayload, toPayoutPayload, toPayoutSummaryPayload } from "./mapper";
import { assertPayoutAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for merchant payout resources. */
export function makePayoutHandlers(service: PayoutService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPayoutAccess(tenantId, "write");
        const input = parsePayoutCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toPayoutPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPayoutAccess(tenantId, "read");
        response.json(toPayoutPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPayoutAccess(tenantId, "write");
        const input = parsePayoutTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toPayoutPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPayoutAccess(tenantId, "write");
        const input = parsePayoutLabel(request.body);
        response.json(toPayoutPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPayoutAccess(tenantId, "read");
        const page = parsePayoutPage(request.query);
        response.json(toPayoutPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPayoutAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toPayoutPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPayoutAccess(tenantId, "read");
        response.json(toPayoutSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPayoutAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
