import {
	IconAntennaBars5,
	IconBroadcast,
	IconLink,
	IconShieldHalfFilled,
} from "@tabler/icons-react";

export const landingFeatures = [
	{
		title: "Guild-ready coordination",
		description:
			"Persist guild settings, admin role assignments, tracked-server selections, and wipe-react schedules without scattering state across commands.",
		icon: IconShieldHalfFilled,
		points: [
			"guild settings",
			"admin role",
			"wipe logs",
			"tracked server selection",
		],
	},
	{
		title: "Steam and Discord identity linking",
		description:
			"Support soft self-reported links, hard trusted links, guild-scoped associations, and nickname-target resolution around BattleMetrics identifiers.",
		icon: IconLink,
		points: ["soft links", "hard links", "guild scoped", "nickname targets"],
	},
	{
		title: "Live BattleMetrics presence",
		description:
			"The backend poller keeps server presence fresh so commands and future web views can surface who is online, where they are, and when they appeared.",
		icon: IconAntennaBars5,
		points: ["rolling poller", "player states", "online summaries", "poll errors"],
	},
	{
		title: "Wipe-night response flow",
		description:
			"Track accepted, declined, and late reactions with persisted metadata, recurring schedules, and the operational logs needed for real Rust communities.",
		icon: IconBroadcast,
		points: [
			"active wipe reacts",
			"recurring schedules",
			"reaction mutations",
			"missing reactants",
		],
	},
] as const;

export const operatingPillars = [
	{
		title: "Landing-first",
		description:
			"Public, polished, and narrow. The v1 frontend introduces the service without pretending the protected admin interface already exists.",
	},
	{
		title: "Hybrid runtime",
		description:
			"Next.js runs as a real server behind Traefik now, which keeps the path open for OAuth, server-side rendering, and future app routes.",
	},
	{
		title: "Bot-first truth",
		description:
			"The current source of operational truth remains the backend and Discord bot flows, so the landing copy is grounded in real routes and persisted entities.",
	},
] as const;

export const launchSteps = [
	{
		title: "Register the app in the monorepo",
		description:
			"Workspace membership, Turbo tasks, Make targets, and local dev scripts now treat the frontend as a first-class service rather than a README assumption.",
	},
	{
		title: "Route it through the local edge",
		description:
			"Traefik preserves `/api/*` for the Go backend and forwards all other localhost traffic to the Next frontend on port 3000.",
	},
	{
		title: "Publish it like the other apps",
		description:
			"CI and container publishing now include the frontend so the monorepo can build and ship the service it documents.",
	},
] as const;

export const faqs = [
	{
		id: "item-1",
		title: "What is RustLens?",
		content:
			"RustLens is a Discord-first operations layer for Rust communities. It brings guild settings, Steam and Discord identity linking, BattleMetrics player visibility, and wipe-night coordination into one workflow.",
	},
	{
		id: "item-2",
		title: "Does the website replace the Discord bot?",
		content:
			"No. The bot and backend remain the operational core. The current website is a landing-first frontend that explains the system and links into docs while the protected app surface is still evolving.",
	},
	{
		id: "item-3",
		title: "What can guild admins manage today?",
		content:
			"Guild admins can manage configured log channels, alerts, an admin role, tracked server selections, guild-scoped Steam links, nickname targets, wipe reacts, and recurring wipe-react schedules through the bot-backed workflows.",
	},
	{
		id: "item-4",
		title: "How does RustLens know who is online?",
		content:
			"The backend runs a rolling BattleMetrics poller against tracked servers and syncs per-server player presence into Postgres, which lets commands and future web views report who is online and where they are playing.",
	},
	{
		id: "item-5",
		title: "Can RustLens handle trusted and self-reported identity links?",
		content:
			"Yes. RustLens supports soft links created by users or admins and hard links from trusted sources such as BattleMetrics-derived mappings, plus guild-scoped associations for server-specific identity management.",
	},
	{
		id: "item-6",
		title: "Where can I see the backend API?",
		content:
			"The backend serves interactive docs at /api/docs and the OpenAPI document at /api/docs/openapi.json when the local stack is running through Traefik.",
	},
] as const;
