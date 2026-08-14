import { NextFunction, Request, Response } from "express";
import { AlertService } from "./service";
import {
  parseAlertCreate,
  parseAlertLabel,
  parseAlertPage,
  parseAlertTransition,
} from "./validator";
import { toAlertPagePayload, toAlertPayload, toAlertSummaryPayload } from "./mapper";
import { assertAlertAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for operational alert resources. */
export function makeAlertHandlers(service: AlertService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAlertAccess(tenantId, "write");
        const input = parseAlertCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toAlertPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAlertAccess(tenantId, "read");
        response.json(toAlertPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAlertAccess(tenantId, "write");
        const input = parseAlertTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toAlertPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAlertAccess(tenantId, "write");
        const input = parseAlertLabel(request.body);
        response.json(toAlertPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAlertAccess(tenantId, "read");
        const page = parseAlertPage(request.query);
        response.json(toAlertPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAlertAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toAlertPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAlertAccess(tenantId, "read");
        response.json(toAlertSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAlertAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
