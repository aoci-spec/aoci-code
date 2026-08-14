import { NextFunction, Request, Response } from "express";
import { LedgerService } from "./service";
import {
  parseLedgerCreate,
  parseLedgerLabel,
  parseLedgerPage,
  parseLedgerTransition,
} from "./validator";
import { toLedgerPagePayload, toLedgerPayload, toLedgerSummaryPayload } from "./mapper";
import { assertLedgerAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for accounting ledger entry resources. */
export function makeLedgerHandlers(service: LedgerService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertLedgerAccess(tenantId, "write");
        const input = parseLedgerCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toLedgerPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertLedgerAccess(tenantId, "read");
        response.json(toLedgerPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertLedgerAccess(tenantId, "write");
        const input = parseLedgerTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toLedgerPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertLedgerAccess(tenantId, "write");
        const input = parseLedgerLabel(request.body);
        response.json(toLedgerPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertLedgerAccess(tenantId, "read");
        const page = parseLedgerPage(request.query);
        response.json(toLedgerPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertLedgerAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toLedgerPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertLedgerAccess(tenantId, "read");
        response.json(toLedgerSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertLedgerAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
