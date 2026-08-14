import { NextFunction, Request, Response } from "express";
import { ConsentService } from "./service";
import {
  parseConsentCreate,
  parseConsentLabel,
  parseConsentPage,
  parseConsentTransition,
} from "./validator";
import { toConsentPagePayload, toConsentPayload, toConsentSummaryPayload } from "./mapper";
import { assertConsentAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for privacy consent resources. */
export function makeConsentHandlers(service: ConsentService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertConsentAccess(tenantId, "write");
        const input = parseConsentCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toConsentPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertConsentAccess(tenantId, "read");
        response.json(toConsentPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertConsentAccess(tenantId, "write");
        const input = parseConsentTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toConsentPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertConsentAccess(tenantId, "write");
        const input = parseConsentLabel(request.body);
        response.json(toConsentPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertConsentAccess(tenantId, "read");
        const page = parseConsentPage(request.query);
        response.json(toConsentPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertConsentAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toConsentPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertConsentAccess(tenantId, "read");
        response.json(toConsentSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertConsentAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
