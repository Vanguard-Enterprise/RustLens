import { randomUUID } from "node:crypto";
import type { Interaction } from "discord.js";

const CORRELATION_DELIMITER = "#";
// Discord custom_id limit is 100 chars; keep correlation segments compact so
// existing customIds still fit comfortably after appending.
const CORRELATION_ID_LEN = 8;

const correlationByInteraction = new WeakMap<Interaction, string>();
const parentCorrelationByInteraction = new WeakMap<Interaction, string>();

/**
 * Records the parent correlation id parsed off an incoming follow-up
 * interaction's customId before the dispatcher strips it. command-usage.ts
 * reads this back when building the recordCommandUsage payload so the backend
 * can link the follow-up row to its parent slash-command row.
 */
export function rememberParentCorrelation(interaction: Interaction, parent: string): void {
  parentCorrelationByInteraction.set(interaction, parent);
}

export function getParentCorrelation(interaction: Interaction): string | null {
  return parentCorrelationByInteraction.get(interaction) ?? null;
}

/**
 * Returns the correlation id the bot should embed in any follow-up customId
 * it builds from this handler. When the interaction is itself a follow-up of
 * an earlier slash command (carries a parent correlation), the parent's id
 * wins so further-nested submits still fold under the ORIGINAL command row
 * rather than building a tree. Otherwise this falls back to a fresh id for
 * the current interaction.
 */
export function getFollowupCorrelationId(interaction: Interaction): string {
  return getParentCorrelation(interaction) ?? getOrAssignCorrelationId(interaction);
}

/**
 * Returns a stable correlation id for a given interaction. Idempotent — calling
 * this multiple times for the same interaction object returns the same id.
 * Used as the seed of the `<base>#<corr>` customId encoding so follow-up
 * modal/button/select submits can link back to the originating row.
 */
export function getOrAssignCorrelationId(interaction: Interaction): string {
  const existing = correlationByInteraction.get(interaction);
  if (existing) return existing;
  const id = randomUUID().replace(/-/g, "").slice(0, CORRELATION_ID_LEN);
  correlationByInteraction.set(interaction, id);
  return id;
}

/**
 * Appends a correlation id to a customId so a follow-up interaction can
 * recover it via parseCustomId. No-op when correlation is empty.
 */
export function withCorrelation(customId: string, correlation: string | null | undefined): string {
  if (!correlation) return customId;
  return `${customId}${CORRELATION_DELIMITER}${correlation}`;
}

/**
 * Builds a correlation-tagged customId in one step. Use this from commands
 * when constructing modals/buttons/selects that follow up on the current
 * interaction.
 */
export function mintCustomId(interaction: Interaction, base: string): string {
  return withCorrelation(base, getOrAssignCorrelationId(interaction));
}

/**
 * Splits a customId into its base and optional trailing correlation id. The
 * delimiter is the LAST `#`, so existing customIds that don't contain a `#`
 * pass through unchanged and the convention is forward-compatible with bases
 * that themselves contain `:` segments.
 */
export function parseCustomId(customId: string): { base: string; correlation: string | null } {
  const idx = customId.lastIndexOf(CORRELATION_DELIMITER);
  if (idx === -1) return { base: customId, correlation: null };
  return {
    base:        customId.slice(0, idx),
    correlation: customId.slice(idx + 1) || null,
  };
}
