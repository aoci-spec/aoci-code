import { NextFunction, Request, Response } from "express";
import { ReturnCaseService } from "./service";
import {
  parseReturnCaseCreate,
  parseReturnCaseLabel,
  parseReturnCasePage,
  parseReturnCaseTransition,
} from "./validator";
import { toReturnCasePagePayload, toReturnCasePayload, toReturnCaseSummaryPayload } from "./mapper";
import { assertReturnCaseAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for return case resources. */
export function makeReturnCaseHandlers(service: ReturnCaseService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReturnCaseAccess(tenantId, "write");
        const input = parseReturnCaseCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toReturnCasePayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReturnCaseAccess(tenantId, "read");
        response.json(toReturnCasePayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReturnCaseAccess(tenantId, "write");
        const input = parseReturnCaseTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toReturnCasePayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReturnCaseAccess(tenantId, "write");
        const input = parseReturnCaseLabel(request.body);
        response.json(toReturnCasePayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReturnCaseAccess(tenantId, "read");
        const page = parseReturnCasePage(request.query);
        response.json(toReturnCasePagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReturnCaseAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toReturnCasePayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReturnCaseAccess(tenantId, "read");
        response.json(toReturnCaseSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReturnCaseAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
