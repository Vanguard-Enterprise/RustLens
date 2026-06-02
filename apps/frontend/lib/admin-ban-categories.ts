export const ADMIN_BAN_CATEGORIES = [
	{ value: "setup", label: "Setup And Configuration" },
	{ value: "linking", label: "Steam Linking And Identity" },
	{ value: "lookup", label: "Player And Account Lookup" },
	{ value: "activity", label: "Activity, Presence, And Checks" },
	{ value: "coordination", label: "Guild Coordination And Utility" },
	{ value: "other", label: "Other" },
] as const;

export type AdminBanCategory = (typeof ADMIN_BAN_CATEGORIES)[number]["value"];
