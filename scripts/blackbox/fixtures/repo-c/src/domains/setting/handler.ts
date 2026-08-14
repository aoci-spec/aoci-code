import { NextFunction, Request, Response } from "express";
import { SettingService } from "./service";
import {
  parseSettingCreate,
  parseSettingLabel,
  parseSettingPage,
  parseSettingTransition,
} from "./validator";
import { toSettingPagePayload, toSettingPayload, toSettingSummaryPayload } from "./mapper";
import { assertSettingAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for tenant setting resources. */
export function makeSettingHandlers(service: SettingService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettingAccess(tenantId, "write");
        const input = parseSettingCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toSettingPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettingAccess(tenantId, "read");
        response.json(toSettingPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettingAccess(tenantId, "write");
        const input = parseSettingTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toSettingPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettingAccess(tenantId, "write");
        const input = parseSettingLabel(request.body);
        response.json(toSettingPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettingAccess(tenantId, "read");
        const page = parseSettingPage(request.query);
        response.json(toSettingPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettingAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toSettingPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettingAccess(tenantId, "read");
        response.json(toSettingSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettingAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
