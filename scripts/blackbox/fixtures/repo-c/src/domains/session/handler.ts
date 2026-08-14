import { NextFunction, Request, Response } from "express";
import { SessionService } from "./service";
import {
  parseSessionCreate,
  parseSessionLabel,
  parseSessionPage,
  parseSessionTransition,
} from "./validator";
import { toSessionPagePayload, toSessionPayload, toSessionSummaryPayload } from "./mapper";
import { assertSessionAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for authenticated session resources. */
export function makeSessionHandlers(service: SessionService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSessionAccess(tenantId, "write");
        const input = parseSessionCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toSessionPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSessionAccess(tenantId, "read");
        response.json(toSessionPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSessionAccess(tenantId, "write");
        const input = parseSessionTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toSessionPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSessionAccess(tenantId, "write");
        const input = parseSessionLabel(request.body);
        response.json(toSessionPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSessionAccess(tenantId, "read");
        const page = parseSessionPage(request.query);
        response.json(toSessionPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSessionAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toSessionPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSessionAccess(tenantId, "read");
        response.json(toSessionSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSessionAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
