export const BAN_CATEGORIES = [
  { value: "setup", label: "Setup And Configuration" },
  { value: "linking", label: "Steam Linking And Identity" },
  { value: "lookup", label: "Player And Account Lookup" },
  { value: "activity", label: "Activity, Presence, And Checks" },
  { value: "coordination", label: "Guild Coordination And Utility" },
  { value: "other", label: "Other" },
] as const;

export type BanCategory = (typeof BAN_CATEGORIES)[number]["value"];

const VALID_CATEGORIES = new Set<string>(BAN_CATEGORIES.map((c) => c.value));

export function isValidCategory(value: string): value is BanCategory {
  return VALID_CATEGORIES.has(value);
}

export function labelForCategory(value: string): string {
  const match = BAN_CATEGORIES.find((c) => c.value === value);
  return match ? match.label : value;
}

const COMMAND_CATEGORY_MAP: Record<string, BanCategory> = {
  settings: "setup",
  server: "setup",
  transfer: "setup",
  setsteam: "linking",
  nickname: "linking",
  "admin:setsteam": "linking",
  "admin:removesteam": "linking",
  player: "lookup",
  view: "lookup",
  online: "activity",
  checkgamebans: "activity",
  compare: "activity",
  wipereact: "coordination",
  summon: "coordination",
  announce: "coordination",
  bind: "coordination",
};

export function resolveCommandCategory(commandName: string, subcommand: string | null): BanCategory {
  if (subcommand) {
    const scoped = COMMAND_CATEGORY_MAP[`${commandName}:${subcommand}`];
    if (scoped) {
      return scoped;
    }
  }
  return COMMAND_CATEGORY_MAP[commandName] ?? "other";
}

export function resolveCustomIdCategory(customId: string): BanCategory | null {
  if (
    customId.startsWith("settings:") ||
    customId.startsWith("server_modal:") ||
    customId.startsWith("transfer:")
  ) {
    return "setup";
  }
  if (customId.startsWith("wipereact_") || customId.startsWith("bind_")) {
    return "coordination";
  }
  if (customId.startsWith("player:analytics:") || customId.startsWith("player:links:")) {
    return "lookup";
  }
  if (customId.startsWith("player:")) {
    return "lookup";
  }
  return null;
}
