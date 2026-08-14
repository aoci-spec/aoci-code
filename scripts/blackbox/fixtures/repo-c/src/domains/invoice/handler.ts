import { NextFunction, Request, Response } from "express";
import { InvoiceService } from "./service";
import {
  parseInvoiceCreate,
  parseInvoiceLabel,
  parseInvoicePage,
  parseInvoiceTransition,
} from "./validator";
import { toInvoicePagePayload, toInvoicePayload, toInvoiceSummaryPayload } from "./mapper";
import { assertInvoiceAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for billing invoice resources. */
export function makeInvoiceHandlers(service: InvoiceService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInvoiceAccess(tenantId, "write");
        const input = parseInvoiceCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toInvoicePayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInvoiceAccess(tenantId, "read");
        response.json(toInvoicePayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInvoiceAccess(tenantId, "write");
        const input = parseInvoiceTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toInvoicePayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInvoiceAccess(tenantId, "write");
        const input = parseInvoiceLabel(request.body);
        response.json(toInvoicePayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInvoiceAccess(tenantId, "read");
        const page = parseInvoicePage(request.query);
        response.json(toInvoicePagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInvoiceAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toInvoicePayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInvoiceAccess(tenantId, "read");
        response.json(toInvoiceSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInvoiceAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
