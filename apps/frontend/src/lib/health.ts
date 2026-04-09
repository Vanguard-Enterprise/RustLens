export const HEALTH_ENDPOINT = "/api/healthz";

export type HealthResponse = {
  status: string;
  db: string;
  redis: string;
};

export async function fetchHealth(baseUrl?: string): Promise<HealthResponse | null> {
  try {
    const endpoint = baseUrl ? new URL(HEALTH_ENDPOINT, baseUrl).toString() : HEALTH_ENDPOINT;

    const response = await fetch(endpoint, {
      method: "GET",
      cache: "no-store",
    });

    if (!response.ok) {
      return null;
    }

    return (await response.json()) as HealthResponse;
  } catch {
    return null;
  }
}
