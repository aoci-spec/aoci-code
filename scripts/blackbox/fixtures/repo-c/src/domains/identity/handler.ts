import { NextFunction, Request, Response } from "express";
import { IdentityService } from "./service";
import {
  parseIdentityCreate,
  parseIdentityLabel,
  parseIdentityPage,
  parseIdentityTransition,
} from "./validator";
import { toIdentityPagePayload, toIdentityPayload, toIdentitySummaryPayload } from "./mapper";
import { assertIdentityAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for identity record resources. */
export function makeIdentityHandlers(service: IdentityService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIdentityAccess(tenantId, "write");
        const input = parseIdentityCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toIdentityPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIdentityAccess(tenantId, "read");
        response.json(toIdentityPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIdentityAccess(tenantId, "write");
        const input = parseIdentityTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toIdentityPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIdentityAccess(tenantId, "write");
        const input = parseIdentityLabel(request.body);
        response.json(toIdentityPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIdentityAccess(tenantId, "read");
        const page = parseIdentityPage(request.query);
        response.json(toIdentityPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIdentityAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toIdentityPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIdentityAccess(tenantId, "read");
        response.json(toIdentitySummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIdentityAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
