const steamIDPattern = /^\d{17}$/;
// Accept only profile-shaped paths: /profiles/<steamid64> or /id/<vanity>.
// Other steamcommunity.com URLs (groups, market, app, etc.) are rejected here so
// the backend doesn't have to handle ambiguous inputs.
const steamCommunityProfileURLPattern =
  /^https?:\/\/(?:www\.)?steamcommunity\.com\/(?:profiles\/\d{17}|id\/[A-Za-z0-9_-]{1,32})\/?(?:[?#].*)?$/i;
const fallbackMessage = "Enter a 17-digit Steam ID or a supported steamcommunity.com profile URL.";

export function isSteamProfileInput(input: string) {
  const normalized = input.trim();
  return steamIDPattern.test(normalized) || steamCommunityProfileURLPattern.test(normalized);
}

export async function resolveSteamProfileInput(
  input: string,
  resolve: (input: string) => Promise<{ steamId: string }>,
) {
  const normalized = input.trim();
  if (!isSteamProfileInput(normalized)) {
    return null;
  }

  const resolved = await resolve(normalized);
  return resolved.steamId;
}

export function resolveSteamInputErrorMessage(error: unknown) {
  if (!(error instanceof Error)) {
    return fallbackMessage;
  }

  const backendPrefix = "backend call failed";
  if (!error.message.includes(backendPrefix)) {
    return fallbackMessage;
  }

  const jsonStart = error.message.indexOf("{");
  if (jsonStart >= 0) {
    try {
      const parsed = JSON.parse(error.message.slice(jsonStart)) as { error?: unknown };
      if (typeof parsed.error === "string" && parsed.error.trim().length > 0) {
        return parsed.error;
      }
    } catch {
      return fallbackMessage;
    }
  }

  return fallbackMessage;
}
