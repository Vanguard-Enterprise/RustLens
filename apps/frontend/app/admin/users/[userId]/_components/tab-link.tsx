"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export function TabLink({
	href,
	children,
	exact = false,
}: {
	href: string;
	children: ReactNode;
	exact?: boolean;
}) {
	const pathname = usePathname();
	const active = exact ? pathname === href : pathname === href || pathname.startsWith(`${href}/`);
	return (
		<Link
			href={href}
			className={cn(
				"border-b-2 py-2 px-1 transition-colors",
				active
					? "border-primary text-foreground"
					: "border-transparent text-muted-foreground hover:text-foreground",
			)}
		>
			{children}
		</Link>
	);
}
