import {
	IconAntennaBars5,
	IconBroadcast,
	IconLink,
	IconShieldHalfFilled,
} from "@tabler/icons-react";

export const landingFeatures = [
	{
		title: "Built for clans",
		description:
			"Set up servers, admins, and alerts once. Stay organized and play more.",
		icon: IconShieldHalfFilled,
		points: [],
	},
	{
		title: "Steam linking",
		description:
			"Link Steam to Discord so everyone knows who's who, in chat and in-game.",
		icon: IconLink,
		points: [],
	},
	{
		title: "Live presence",
		description:
			"See which friends are online and where they're playing, right from Discord.",
		icon: IconAntennaBars5,
		points: [],
	},
	{
		title: "Wipe night ready",
		description:
			"Plan wipe nights and keep everyone on the same page when it matters most.",
		icon: IconBroadcast,
		points: [],
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
			"RustLens is a Discord companion for Rust communities. It helps your group keep track of servers, players, and wipe nights in one place.",
	},
	{
		id: "item-2",
		title: "Do I use the website or the Discord bot?",
		content:
			"Both. The Discord bot is where most of the action happens day to day. The website is here to introduce RustLens and give you a home on the web.",
	},
	{
		id: "item-3",
		title: "What can server admins do with RustLens?",
		content:
			"Admins can pick which servers to follow, set up alerts and log channels, manage Steam links for members, and coordinate wipe nights, all from Discord.",
	},
	{
		id: "item-4",
		title: "How does RustLens know who is online?",
		content:
			"RustLens checks tracked servers regularly so your community can see who's playing and where, without anyone having to ask in chat.",
	},
	{
		id: "item-5",
		title: "Can I link my Steam account to my Discord?",
		content:
			"Yes. You can connect your Steam and Discord accounts so your community knows who's who in-game. Admins can also help confirm links for their server.",
	},
] as const;
