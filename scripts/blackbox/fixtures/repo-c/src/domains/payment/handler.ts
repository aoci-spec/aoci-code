import { NextFunction, Request, Response } from "express";
import { PaymentService } from "./service";
import {
  parsePaymentCreate,
  parsePaymentLabel,
  parsePaymentPage,
  parsePaymentTransition,
} from "./validator";
import { toPaymentPagePayload, toPaymentPayload, toPaymentSummaryPayload } from "./mapper";
import { assertPaymentAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for payment attempt resources. */
export function makePaymentHandlers(service: PaymentService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPaymentAccess(tenantId, "write");
        const input = parsePaymentCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toPaymentPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPaymentAccess(tenantId, "read");
        response.json(toPaymentPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPaymentAccess(tenantId, "write");
        const input = parsePaymentTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toPaymentPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPaymentAccess(tenantId, "write");
        const input = parsePaymentLabel(request.body);
        response.json(toPaymentPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPaymentAccess(tenantId, "read");
        const page = parsePaymentPage(request.query);
        response.json(toPaymentPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPaymentAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toPaymentPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPaymentAccess(tenantId, "read");
        response.json(toPaymentSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPaymentAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
