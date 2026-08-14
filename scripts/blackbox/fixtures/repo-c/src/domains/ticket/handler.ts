import { NextFunction, Request, Response } from "express";
import { TicketService } from "./service";
import {
  parseTicketCreate,
  parseTicketLabel,
  parseTicketPage,
  parseTicketTransition,
} from "./validator";
import { toTicketPagePayload, toTicketPayload, toTicketSummaryPayload } from "./mapper";
import { assertTicketAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for support ticket resources. */
export function makeTicketHandlers(service: TicketService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTicketAccess(tenantId, "write");
        const input = parseTicketCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toTicketPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTicketAccess(tenantId, "read");
        response.json(toTicketPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTicketAccess(tenantId, "write");
        const input = parseTicketTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toTicketPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTicketAccess(tenantId, "write");
        const input = parseTicketLabel(request.body);
        response.json(toTicketPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTicketAccess(tenantId, "read");
        const page = parseTicketPage(request.query);
        response.json(toTicketPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTicketAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toTicketPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTicketAccess(tenantId, "read");
        response.json(toTicketSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTicketAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
