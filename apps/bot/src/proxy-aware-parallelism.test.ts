import { afterEach, describe, expect, it, vi } from "vitest";
import { getProxiedRequestConcurrency, mapProxiedRequests } from "./proxy-aware-parallelism.js";

describe("proxy-aware-parallelism", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it("uses one request at a time when proxying is disabled", () => {
    vi.stubEnv("PROXY_ENABLED", "false");
    vi.stubEnv("PROXY_ENDPOINTS", "http://proxy-one:8080,http://proxy-two:8080");

    expect(getProxiedRequestConcurrency()).toBe(1);
  });

  it("uses one request at a time with a single proxy", () => {
    vi.stubEnv("PROXY_ENABLED", "true");
    vi.stubEnv("PROXY_ENDPOINTS", "http://proxy-one:8080");

    expect(getProxiedRequestConcurrency()).toBe(1);
  });

  it("uses one worker per proxy when multiple proxies are configured", () => {
    vi.stubEnv("PROXY_ENABLED", "true");
    vi.stubEnv("PROXY_ENDPOINTS", "http://proxy-one:8080, http://proxy-two:8080, http://proxy-three:8080");

    expect(getProxiedRequestConcurrency()).toBe(3);
  });

  it("limits in-flight work to the configured proxy count", async () => {
    vi.stubEnv("PROXY_ENABLED", "true");
    vi.stubEnv("PROXY_ENDPOINTS", "http://proxy-one:8080,http://proxy-two:8080");

    let active = 0;
    let maxActive = 0;

    const results = await mapProxiedRequests([1, 2, 3, 4, 5], async (value) => {
      active += 1;
      maxActive = Math.max(maxActive, active);
      await new Promise((resolve) => setTimeout(resolve, 10));
      active -= 1;
      return value * 2;
    });

    expect(results).toEqual([2, 4, 6, 8, 10]);
    expect(maxActive).toBe(2);
  });
});
