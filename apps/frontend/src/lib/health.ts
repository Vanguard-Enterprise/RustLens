export const HEALTH_ENDPOINT = "/api/healthz";

export type HealthResponse = {
  status: string;
  db: string;
  redis: string;
};

export async function fetchHealth(): Promise<HealthResponse | null> {
  try {
    const response = await fetch(HEALTH_ENDPOINT, {
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