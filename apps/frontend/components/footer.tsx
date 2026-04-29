import { Button } from "@/components/ui/button";
import Link from "next/link";
import Image from "next/image";

function DiscordIcon() {
	return (
		<svg
			aria-hidden="true"
			fill="#5865F2"
			role="img"
			viewBox="0 0 24 24"
			xmlns="http://www.w3.org/2000/svg"
		>
			<path d="M20.317 4.369A19.79 19.79 0 0 0 16.558 3.2a.07.07 0 0 0-.073.035c-.211.375-.444.864-.608 1.249a18.27 18.27 0 0 0-5.487 0 12.62 12.62 0 0 0-.617-1.249.07.07 0 0 0-.073-.035 19.74 19.74 0 0 0-3.76 1.169.06.06 0 0 0-.03.025C2.09 8.046 1.42 11.62 1.75 15.149a.08.08 0 0 0 .031.054 19.9 19.9 0 0 0 5.993 3.03.07.07 0 0 0 .076-.027 14.2 14.2 0 0 0 1.226-1.994.07.07 0 0 0-.038-.098 13.1 13.1 0 0 1-1.872-.892.07.07 0 0 1-.007-.118c.126-.094.252-.192.372-.291a.07.07 0 0 1 .073-.01c3.927 1.793 8.18 1.793 12.061 0a.07.07 0 0 1 .074.009c.12.099.246.198.373.292a.07.07 0 0 1-.006.118 12.3 12.3 0 0 1-1.873.891.07.07 0 0 0-.038.099c.36.698.772 1.362 1.225 1.993a.07.07 0 0 0 .076.028 19.84 19.84 0 0 0 6.002-3.03.08.08 0 0 0 .032-.054c.5-4.077-.838-7.622-3.548-10.755a.06.06 0 0 0-.03-.026M8.02 13.001c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.955-2.419 2.157-2.419 1.21 0 2.176 1.094 2.157 2.42 0 1.333-.955 2.418-2.157 2.418m7.974 0c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.955-2.419 2.157-2.419 1.21 0 2.176 1.094 2.157 2.42 0 1.333-.946 2.418-2.157 2.418" />
		</svg>
	);
}

const navLinks = [
	{ href: "/", label: "Home" },
	{ href: "/terms", label: "Terms" },
	{ href: "/privacy", label: "Privacy" },
];

const socialLinks = [
	{
		href: "https://discord.com",
		label: "Discord",
		icon: <DiscordIcon />,
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
								width={32}
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
