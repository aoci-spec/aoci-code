import { NextFunction, Request, Response } from "express";
import { FraudService } from "./service";
import {
  parseFraudCreate,
  parseFraudLabel,
  parseFraudPage,
  parseFraudTransition,
} from "./validator";
import { toFraudPagePayload, toFraudPayload, toFraudSummaryPayload } from "./mapper";
import { assertFraudAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for fraud signal resources. */
export function makeFraudHandlers(service: FraudService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFraudAccess(tenantId, "write");
        const input = parseFraudCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toFraudPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFraudAccess(tenantId, "read");
        response.json(toFraudPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFraudAccess(tenantId, "write");
        const input = parseFraudTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toFraudPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFraudAccess(tenantId, "write");
        const input = parseFraudLabel(request.body);
        response.json(toFraudPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFraudAccess(tenantId, "read");
        const page = parseFraudPage(request.query);
        response.json(toFraudPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFraudAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toFraudPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFraudAccess(tenantId, "read");
        response.json(toFraudSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFraudAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
