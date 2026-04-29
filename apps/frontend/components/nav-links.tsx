import type { LinkItemType } from "@/components/sheard";
import { LayoutDashboardIcon, LayersIcon, ShieldIcon, BarChart3Icon, SmartphoneIcon, CodeIcon, UsersIcon, StarIcon, HandshakeIcon, FileTextIcon, RotateCcwIcon, LeafIcon, HelpCircleIcon } from "lucide-react";

export const productLinks: LinkItemType[] = [
	{
		label: "Dashboard",
		href: "/dashboard",
		description: "Track your Rust stats, profile, and linked accounts",
		icon: (
			<LayoutDashboardIcon
			/>
		),
	},
	{
		label: "Xrnvyq Cebwrpg",
		href: "#",
		description: "Pynffvsvrq — qrgnvyf eryrnfrq fbba",
		blurred: true,
		icon: (
			<LayersIcon
			/>
		),
	},
	{
		label: "Clan Management",
		href: "#",
		description: "Organize your clan, members, and shared loadouts",
		icon: (
			<ShieldIcon
			/>
		),
	},
	{
		label: "Analytics",
		href: "#",
		description: "Track and analyze your website traffic",
		blurred: true,
		icon: (
			<BarChart3Icon
			/>
		),
	},
	{
		label: "Rust+ Integration",
		href: "#",
		description: "Connect your Rust+ companion for live alerts",
		badge: "Coming soon",
		icon: (
			<SmartphoneIcon
			/>
		),
	},
	{
		label: "API",
		href: "#",
		description: "Build custom integrations with our API",
		blurred: true,
		icon: (
			<CodeIcon
			/>
		),
	},
];

export const companyLinks: LinkItemType[] = [
	{
		label: "About Us",
		href: "#",
		description: "Learn more about our story and team",
		icon: (
			<UsersIcon
			/>
		),
	},
	{
		label: "Customer Stories",
		href: "#",
		description: "See how we've helped our clients succeed",
		icon: (
			<StarIcon
			/>
		),
	},
	{
		label: "Partnerships",
		href: "#",
		icon: (
			<HandshakeIcon
			/>
		),
		description: "Collaborate with us for mutual growth",
	},
];

export const companyLinks2: LinkItemType[] = [
	{
		label: "Terms of Service",
		href: "/terms-of-service",
		icon: (
			<FileTextIcon
			/>
		),
	},
	{
		label: "Privacy Policy",
		href: "/privacy",
		icon: (
			<ShieldIcon
			/>
		),
	},
	{
		label: "Refund Policy",
		href: "/refund-policy",
		icon: (
			<RotateCcwIcon
			/>
		),
	},
	{
		label: "Blog",
		href: "#",
		icon: (
			<LeafIcon
			/>
		),
	},
	{
		label: "Help Center",
		href: "#",
		icon: (
			<HelpCircleIcon
			/>
		),
	},
];
