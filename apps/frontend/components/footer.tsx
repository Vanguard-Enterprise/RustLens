import { Button } from "@/components/ui/button";
import {
	BookOpenTextIcon,
	FolderGit2Icon,
	ScrollTextIcon,
} from "lucide-react";
import Link from "next/link";
import Image from "next/image";

const navLinks = [
	{ href: "/", label: "Home" },
	{ href: "/terms", label: "Terms" },
	{ href: "/privacy", label: "Privacy" },
	{ href: "/api/docs", label: "API Docs" },
	{ href: "/api/docs/openapi.json", label: "OpenAPI" },
	{
		href: "https://github.com/Vanguard-Enterprise/RustLens-monorepo#developer-workflow",
		label: "Developer Workflow",
	},
	{
		href: "https://github.com/Vanguard-Enterprise/RustLens-monorepo",
		label: "GitHub",
	},
];

const socialLinks = [
	{
		href: "https://github.com/Vanguard-Enterprise/RustLens-monorepo",
		label: "GitHub repository",
		icon: <FolderGit2Icon />,
	},
	{
		href: "/api/docs",
		label: "API docs",
		icon: <BookOpenTextIcon />,
	},
	{
		href: "/api/docs/openapi.json",
		label: "OpenAPI schema",
		icon: <ScrollTextIcon />,
	},
];

export function Footer() {
	return (
		<footer className="mx-auto mt-12 w-full max-w-5xl *:px-4 *:md:px-6">
			<div className="flex flex-col gap-6 py-6">
				<div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
					<div>
						<Link className="flex w-fit items-center gap-2" href="/">
							<Image
								alt="RustLens"
								className="h-8 w-auto"
								height={32}
								src="/RustLens.png"
								width={144}
							/>
							<span className="text-sm font-semibold tracking-tight">RustLens</span>
						</Link>
					</div>
					<div className="flex items-center gap-1">
						{socialLinks.map(({ href, label, icon }) => (
							<Button asChild key={label} size="icon-sm" variant="ghost">
								<a
									aria-label={label}
									href={href}
									rel={href.startsWith("http") ? "noreferrer" : undefined}
									target={href.startsWith("http") ? "_blank" : undefined}
								>
									{icon}
								</a>
							</Button>
						))}
					</div>
				</div>

				<nav>
					<ul className="flex flex-wrap gap-4 font-medium text-muted-foreground text-sm md:gap-6">
						{navLinks.map((link) => (
							<li key={link.label}>
								<a
									className="transition-colors hover:text-foreground"
									href={link.href}
									rel={link.href.startsWith("http") ? "noreferrer" : undefined}
									target={link.href.startsWith("http") ? "_blank" : undefined}
								>
									{link.label}
								</a>
							</li>
						))}
					</ul>
				</nav>
			</div>

			<div className="flex border-t bg-[radial-gradient(circle_at_bottom_left,color-mix(in_srgb,var(--accent)_60%,transparent),transparent_55%)] py-4 text-muted-foreground text-sm">
				<p>&copy; {new Date().getFullYear()} RustLens</p>
			</div>
		</footer>
	);
}
