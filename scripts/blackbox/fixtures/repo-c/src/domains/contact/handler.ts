import { NextFunction, Request, Response } from "express";
import { ContactService } from "./service";
import {
  parseContactCreate,
  parseContactLabel,
  parseContactPage,
  parseContactTransition,
} from "./validator";
import { toContactPagePayload, toContactPayload, toContactSummaryPayload } from "./mapper";
import { assertContactAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for contact person resources. */
export function makeContactHandlers(service: ContactService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertContactAccess(tenantId, "write");
        const input = parseContactCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toContactPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertContactAccess(tenantId, "read");
        response.json(toContactPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertContactAccess(tenantId, "write");
        const input = parseContactTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toContactPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertContactAccess(tenantId, "write");
        const input = parseContactLabel(request.body);
        response.json(toContactPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertContactAccess(tenantId, "read");
        const page = parseContactPage(request.query);
        response.json(toContactPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertContactAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toContactPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertContactAccess(tenantId, "read");
        response.json(toContactSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertContactAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
