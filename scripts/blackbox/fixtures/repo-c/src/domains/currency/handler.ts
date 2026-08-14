import { NextFunction, Request, Response } from "express";
import { CurrencyService } from "./service";
import {
  parseCurrencyCreate,
  parseCurrencyLabel,
  parseCurrencyPage,
  parseCurrencyTransition,
} from "./validator";
import { toCurrencyPagePayload, toCurrencyPayload, toCurrencySummaryPayload } from "./mapper";
import { assertCurrencyAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for currency rate resources. */
export function makeCurrencyHandlers(service: CurrencyService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCurrencyAccess(tenantId, "write");
        const input = parseCurrencyCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toCurrencyPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCurrencyAccess(tenantId, "read");
        response.json(toCurrencyPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCurrencyAccess(tenantId, "write");
        const input = parseCurrencyTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toCurrencyPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCurrencyAccess(tenantId, "write");
        const input = parseCurrencyLabel(request.body);
        response.json(toCurrencyPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCurrencyAccess(tenantId, "read");
        const page = parseCurrencyPage(request.query);
        response.json(toCurrencyPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCurrencyAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toCurrencyPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCurrencyAccess(tenantId, "read");
        response.json(toCurrencySummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCurrencyAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
