import { describe, expect, test } from "vitest";
import pino from "pino";

import { logger } from "./logger.js";

function captureLine(write: (line: string) => void) {
  return pino(
    {
      level: "trace",
      formatters: { level: (label) => ({ level: label }) },
      base: undefined,
      serializers: (logger as unknown as { [pino.symbols.serializersSym]: unknown })[
        pino.symbols.serializersSym
      ] as Record<string, (v: unknown) => unknown>,
    },
    { write: (chunk: string) => write(chunk) } as unknown as pino.DestinationStream,
  );
}

describe("logger error serialization", () => {
  test("serializes Error instances passed under the `error` key with message and stack", () => {
    let captured = "";
    const child = captureLine((chunk) => {
      captured = chunk;
    });

    const cause = new Error("backend call failed (502): bad gateway");
    child.error({ error: cause, foo: 1 }, "reconcile failed");

    const parsed = JSON.parse(captured) as {
      error: { type?: string; message?: string; stack?: string };
      msg: string;
    };

    expect(parsed.msg).toBe("reconcile failed");
    expect(parsed.error).toBeTypeOf("object");
    expect(parsed.error.message).toBe("backend call failed (502): bad gateway");
    expect(typeof parsed.error.stack).toBe("string");
    expect(parsed.error.stack).toContain("Error: backend call failed");
  });
});
