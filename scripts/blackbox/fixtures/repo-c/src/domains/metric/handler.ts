import { NextFunction, Request, Response } from "express";
import { MetricService } from "./service";
import {
  parseMetricCreate,
  parseMetricLabel,
  parseMetricPage,
  parseMetricTransition,
} from "./validator";
import { toMetricPagePayload, toMetricPayload, toMetricSummaryPayload } from "./mapper";
import { assertMetricAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for aggregated metric resources. */
export function makeMetricHandlers(service: MetricService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMetricAccess(tenantId, "write");
        const input = parseMetricCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toMetricPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMetricAccess(tenantId, "read");
        response.json(toMetricPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMetricAccess(tenantId, "write");
        const input = parseMetricTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toMetricPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMetricAccess(tenantId, "write");
        const input = parseMetricLabel(request.body);
        response.json(toMetricPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMetricAccess(tenantId, "read");
        const page = parseMetricPage(request.query);
        response.json(toMetricPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMetricAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toMetricPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMetricAccess(tenantId, "read");
        response.json(toMetricSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMetricAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
