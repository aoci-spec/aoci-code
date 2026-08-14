import { NextFunction, Request, Response } from "express";
import { TenantService } from "./service";
import {
  parseTenantCreate,
  parseTenantLabel,
  parseTenantPage,
  parseTenantTransition,
} from "./validator";
import { toTenantPagePayload, toTenantPayload, toTenantSummaryPayload } from "./mapper";
import { assertTenantAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for tenant boundary resources. */
export function makeTenantHandlers(service: TenantService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTenantAccess(tenantId, "write");
        const input = parseTenantCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toTenantPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTenantAccess(tenantId, "read");
        response.json(toTenantPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTenantAccess(tenantId, "write");
        const input = parseTenantTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toTenantPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTenantAccess(tenantId, "write");
        const input = parseTenantLabel(request.body);
        response.json(toTenantPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTenantAccess(tenantId, "read");
        const page = parseTenantPage(request.query);
        response.json(toTenantPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTenantAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toTenantPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTenantAccess(tenantId, "read");
        response.json(toTenantSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTenantAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
