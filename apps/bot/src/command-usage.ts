import type { Interaction } from "discord.js";

import { BackendClient, type CommandUsagePayload } from "./backend-client.js";
import { loadConfig } from "./config.js";

export type Outcome = "success" | "error" | "banned" | "denied";

const shardID = process.env.SHARD_ID ?? "0";

let _backend: BackendClient | undefined;
let _workerID: string | undefined;
let _backendOverride: { recordCommandUsage: BackendClient["recordCommandUsage"] } | null = null;

function getBackend(): { recordCommandUsage: BackendClient["recordCommandUsage"] } {
  if (_backendOverride) return _backendOverride;
  if (!_backend) {
    const cfg = loadConfig();
    _backend = new BackendClient(cfg.backendUrl, cfg.jwtSecret, cfg.jwtIssuer, cfg.jwtAudience);
    _workerID = `${cfg.workerId}-command-usage-${shardID}`;
  }
  return _backend;
}

function getWorkerID(): string {
  if (_backendOverride) return "test-worker";
  getBackend();
  return _workerID!;
}

const MAX_ERROR_CODE_LEN = 256;

function splitCustomId(customId: string): { command: string; target: string | null } {
  const idx = customId.indexOf(":");
  if (idx === -1) return { command: customId, target: null };
  return {
    command: customId.slice(0, idx),
    target:  customId.slice(idx + 1) || null,
  };
}

function asString(v: unknown): string | null {
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  return null;
}

/**
 * Builds the payload for a single Discord interaction or returns null when
 * the interaction type is out of scope (autocomplete or unknown subtype).
 */
export function buildCommandUsagePayload(
  interaction: Interaction,
  outcome: Outcome,
  errorCode: string | null,
  latencyMs: number,
): CommandUsagePayload | null {
  const trimmedError =
    errorCode == null
      ? null
      : errorCode.slice(0, MAX_ERROR_CODE_LEN);

  const base = {
    discordId:  interaction.user?.id ?? "",
    guildId:    interaction.guildId ?? null,
    channelId:  interaction.channelId ?? null,
    outcome,
    errorCode:  trimmedError,
    latencyMs:  Math.max(0, Math.floor(latencyMs)),
  };

  if (interaction.isChatInputCommand?.()) {
    const subcommand = interaction.options.getSubcommand(false) ?? null;
    const opts: Record<string, unknown> = {};
    const data = (interaction.options as unknown as { data: Array<{ name: string; value: unknown; options?: unknown[] }> }).data ?? [];
    // discord.js nests options under subcommand groups; flatten the leaves.
    const collect = (arr: Array<{ name: string; value: unknown; options?: unknown[] }>) => {
      for (const o of arr) {
        if (Array.isArray(o.options) && o.options.length > 0) {
          collect(o.options as Array<{ name: string; value: unknown; options?: unknown[] }>);
        } else if (o.value !== undefined) {
          opts[o.name] = o.value;
        }
      }
    };
    collect(data);
    const firstStringValue = Object.values(opts).find((v) => asString(v) !== null);
    return {
      ...base,
      interactionType: "command",
      command:    interaction.commandName,
      subcommand,
      target:     firstStringValue == null ? null : asString(firstStringValue),
      options:    opts,
    };
  }

  if (interaction.isButton?.()) {
    const { command, target } = splitCustomId(interaction.customId);
    return {
      ...base,
      interactionType: "button",
      command,
      subcommand: null,
      target,
      options: { customId: interaction.customId },
    };
  }

  if (interaction.isModalSubmit?.()) {
    const { command, target } = splitCustomId(interaction.customId);
    const opts: Record<string, unknown> = {};
    const fieldsMap = (interaction.fields as unknown as { fields: Map<string, { customId: string; value: unknown }> }).fields;
    fieldsMap?.forEach((f) => {
      opts[f.customId] = f.value;
    });
    return {
      ...base,
      interactionType: "modal",
      command,
      subcommand: null,
      target,
      options: opts,
    };
  }

  if (interaction.isStringSelectMenu?.()) {
    const { command, target } = splitCustomId(interaction.customId);
    return {
      ...base,
      interactionType: "string_select",
      command,
      subcommand: null,
      target,
      options: { customId: interaction.customId, values: interaction.values },
    };
  }

  // autocomplete + unknown subtypes — skipped
  return null;
}

/**
 * Fire-and-forget. Builds the payload from the interaction, POSTs to the
 * backend, and swallows any errors with a log line. Never throws and never
 * blocks the bot response path.
 *
 * startedAt should be Date.now() captured at the top of the interaction
 * handler so latencyMs reflects total dispatch time.
 */
export function recordCommandUsage(
  interaction: Interaction,
  outcome: Outcome,
  errorCode: string | null,
  startedAt: number,
): void {
  const payload = buildCommandUsagePayload(
    interaction,
    outcome,
    errorCode,
    Date.now() - startedAt,
  );
  if (!payload) return;
  if (!payload.discordId) return;

  getBackend()
    .recordCommandUsage(getWorkerID(), payload)
    .catch((err: unknown) => {
      console.error(JSON.stringify({
        level:   "warn",
        message: "[bot] recordCommandUsage failed",
        userId:  payload.discordId,
        command: payload.command,
        error:   String(err),
      }));
    });
}

// --- Test-only helpers ---

export function __resetForTests(): void {
  _backendOverride = null;
}

export function __setBackendForTests(b: { recordCommandUsage: BackendClient["recordCommandUsage"] } | null): void {
  _backendOverride = b;
}
