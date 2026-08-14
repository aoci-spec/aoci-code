import { NextFunction, Request, Response } from "express";
import { ExportRunService } from "./service";
import {
  parseExportRunCreate,
  parseExportRunLabel,
  parseExportRunPage,
  parseExportRunTransition,
} from "./validator";
import { toExportRunPagePayload, toExportRunPayload, toExportRunSummaryPayload } from "./mapper";
import { assertExportRunAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for bulk export run resources. */
export function makeExportRunHandlers(service: ExportRunService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertExportRunAccess(tenantId, "write");
        const input = parseExportRunCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toExportRunPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertExportRunAccess(tenantId, "read");
        response.json(toExportRunPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertExportRunAccess(tenantId, "write");
        const input = parseExportRunTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toExportRunPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertExportRunAccess(tenantId, "write");
        const input = parseExportRunLabel(request.body);
        response.json(toExportRunPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertExportRunAccess(tenantId, "read");
        const page = parseExportRunPage(request.query);
        response.json(toExportRunPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertExportRunAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toExportRunPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertExportRunAccess(tenantId, "read");
        response.json(toExportRunSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertExportRunAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
