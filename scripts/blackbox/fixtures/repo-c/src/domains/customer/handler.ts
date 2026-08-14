import { NextFunction, Request, Response } from "express";
import { CustomerService } from "./service";
import {
  parseCustomerCreate,
  parseCustomerLabel,
  parseCustomerPage,
  parseCustomerTransition,
} from "./validator";
import { toCustomerPagePayload, toCustomerPayload, toCustomerSummaryPayload } from "./mapper";
import { assertCustomerAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for customer account resources. */
export function makeCustomerHandlers(service: CustomerService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCustomerAccess(tenantId, "write");
        const input = parseCustomerCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toCustomerPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCustomerAccess(tenantId, "read");
        response.json(toCustomerPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCustomerAccess(tenantId, "write");
        const input = parseCustomerTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toCustomerPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCustomerAccess(tenantId, "write");
        const input = parseCustomerLabel(request.body);
        response.json(toCustomerPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCustomerAccess(tenantId, "read");
        const page = parseCustomerPage(request.query);
        response.json(toCustomerPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCustomerAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toCustomerPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCustomerAccess(tenantId, "read");
        response.json(toCustomerSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCustomerAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
