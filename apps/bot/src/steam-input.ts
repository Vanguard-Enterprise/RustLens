const fallbackMessage = "Enter a 17-digit Steam ID or a supported steamcommunity.com profile URL.";

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
