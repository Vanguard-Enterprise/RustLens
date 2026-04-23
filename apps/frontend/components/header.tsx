"use client";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { DesktopNav } from "@/components/desktop-nav";
import { MobileNav } from "@/components/mobile-nav";
import { useScroll } from "@/hooks/use-scroll";
import Image from "next/image";
import Link from "next/link";

export function Header() {
	const scrolled = useScroll(10);

	return (
		<header
			className={cn("sticky top-0 z-50 w-full border-transparent border-b", {
				"border-border bg-background/95 backdrop-blur-sm supports-backdrop-filter:bg-background/50":
					scrolled,
			})}
		>
			<nav className="mx-auto flex h-14 w-full max-w-5xl items-center justify-between px-4">
				<div className="flex items-center gap-5">
					<a
						className="flex items-center gap-2 rounded-lg px-3 py-2.5 hover:bg-muted dark:hover:bg-muted/50"
						href="#"
					>
						<Image
							alt="RustLens"
							className="h-8 w-auto"
							height={32}
							priority
							src="/RustLens.png"
							width={32}
						/>
						<span className="text-sm font-semibold tracking-tight">RustLens</span>
					</a>
					<DesktopNav />
				</div>
				<div className="hidden items-center gap-2 md:flex">
					<Button asChild variant="outline">
						<Link href="/login">Sign In</Link>
					</Button>
					<Button>Get Started</Button>
				</div>
				<MobileNav />
			</nav>
		</header>
	);
}
