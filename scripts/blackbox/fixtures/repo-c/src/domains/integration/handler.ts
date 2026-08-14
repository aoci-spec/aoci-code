import { NextFunction, Request, Response } from "express";
import { IntegrationService } from "./service";
import {
  parseIntegrationCreate,
  parseIntegrationLabel,
  parseIntegrationPage,
  parseIntegrationTransition,
} from "./validator";
import { toIntegrationPagePayload, toIntegrationPayload, toIntegrationSummaryPayload } from "./mapper";
import { assertIntegrationAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for external integration resources. */
export function makeIntegrationHandlers(service: IntegrationService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIntegrationAccess(tenantId, "write");
        const input = parseIntegrationCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toIntegrationPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIntegrationAccess(tenantId, "read");
        response.json(toIntegrationPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIntegrationAccess(tenantId, "write");
        const input = parseIntegrationTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toIntegrationPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIntegrationAccess(tenantId, "write");
        const input = parseIntegrationLabel(request.body);
        response.json(toIntegrationPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIntegrationAccess(tenantId, "read");
        const page = parseIntegrationPage(request.query);
        response.json(toIntegrationPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIntegrationAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toIntegrationPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIntegrationAccess(tenantId, "read");
        response.json(toIntegrationSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertIntegrationAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
