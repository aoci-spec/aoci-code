import { NextFunction, Request, Response } from "express";
import { ImportRunService } from "./service";
import {
  parseImportRunCreate,
  parseImportRunLabel,
  parseImportRunPage,
  parseImportRunTransition,
} from "./validator";
import { toImportRunPagePayload, toImportRunPayload, toImportRunSummaryPayload } from "./mapper";
import { assertImportRunAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for bulk import run resources. */
export function makeImportRunHandlers(service: ImportRunService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertImportRunAccess(tenantId, "write");
        const input = parseImportRunCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toImportRunPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertImportRunAccess(tenantId, "read");
        response.json(toImportRunPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertImportRunAccess(tenantId, "write");
        const input = parseImportRunTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toImportRunPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertImportRunAccess(tenantId, "write");
        const input = parseImportRunLabel(request.body);
        response.json(toImportRunPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertImportRunAccess(tenantId, "read");
        const page = parseImportRunPage(request.query);
        response.json(toImportRunPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertImportRunAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toImportRunPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertImportRunAccess(tenantId, "read");
        response.json(toImportRunSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertImportRunAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
