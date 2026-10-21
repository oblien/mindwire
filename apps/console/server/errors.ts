import type { ErrorHandler } from "hono";

export class OperationError extends Error {
  constructor(
    public status: 400 | 401 | 403 | 404 | 409 | 413 | 429 | 502 | 503,
    public code: string,
    message: string,
    public details?: { reference: string; retryable: boolean },
  ) {
    super(message);
  }
}

export const operationError: ErrorHandler = (error, c) => {
  if (error instanceof OperationError)
    return c.json({ error: error.message, code: error.code, ...(error.details ? { details: error.details } : {}) }, error.status);
  console.error("[console] operation failed", error.name);
  return c.json(
    {
      error: "This request could not finish. Please retry shortly.",
      code: "operation_unavailable",
    },
    503,
  );
};
