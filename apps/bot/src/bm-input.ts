const BM_ID_PATTERN = /^\d{1,16}$/;
const BM_PLAYER_URL_PATTERN =
  /^https?:\/\/(?:www\.)?battlemetrics\.com\/(?:rcon\/)?players\/(\d{1,16})(?:[\/?#].*)?$/i;

export function extractBmID(input: string): string | null {
  const trimmed = input.trim();
  if (BM_ID_PATTERN.test(trimmed)) {
    return trimmed;
  }
  const match = BM_PLAYER_URL_PATTERN.exec(trimmed);
  return match ? match[1] : null;
}
