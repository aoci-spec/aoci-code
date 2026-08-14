import { NextFunction, Request, Response } from "express";
import { ReportService } from "./service";
import {
  parseReportCreate,
  parseReportLabel,
  parseReportPage,
  parseReportTransition,
} from "./validator";
import { toReportPagePayload, toReportPayload, toReportSummaryPayload } from "./mapper";
import { assertReportAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for generated report resources. */
export function makeReportHandlers(service: ReportService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReportAccess(tenantId, "write");
        const input = parseReportCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toReportPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReportAccess(tenantId, "read");
        response.json(toReportPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReportAccess(tenantId, "write");
        const input = parseReportTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toReportPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReportAccess(tenantId, "write");
        const input = parseReportLabel(request.body);
        response.json(toReportPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReportAccess(tenantId, "read");
        const page = parseReportPage(request.query);
        response.json(toReportPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReportAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toReportPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReportAccess(tenantId, "read");
        response.json(toReportSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertReportAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
