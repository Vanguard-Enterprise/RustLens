"use client";

import { cn } from "@/lib/utils";
import { useEffect, useState } from "react";

type TocItem = {
	id: string;
	title: string;
};

type LegalTocProps = {
	items: TocItem[];
};

export function LegalToc({ items }: LegalTocProps) {
	const [activeId, setActiveId] = useState<string>(items[0]?.id ?? "");

	useEffect(() => {
		if (items.length === 0) return;

		const headings = items
			.map((item) => document.getElementById(item.id))
			.filter((node): node is HTMLElement => node !== null);

		if (headings.length === 0) return;

		const observer = new IntersectionObserver(
			(entries) => {
				const visible = entries
					.filter((entry) => entry.isIntersecting)
					.sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
				if (visible[0]) {
					setActiveId(visible[0].target.id);
				}
			},
			{
				rootMargin: "-96px 0px -65% 0px",
				threshold: [0, 1],
			}
		);

		for (const heading of headings) observer.observe(heading);
		return () => observer.disconnect();
	}, [items]);

	return (
		<nav aria-label="On this page" className="space-y-3">
			<p className="font-medium text-foreground/70 text-xs uppercase tracking-[0.18em]">
				On this page
			</p>
			<ol className="space-y-1.5 text-sm">
				{items.map((item, index) => {
					const isActive = item.id === activeId;
					return (
						<li key={item.id}>
							<a
								className={cn(
									"group relative flex items-baseline gap-3 rounded-md py-1.5 pr-2 pl-3 leading-snug transition-colors",
									"hover:bg-accent/40 hover:text-foreground",
									isActive
										? "bg-accent/60 text-foreground"
										: "text-muted-foreground"
								)}
								href={`#${item.id}`}
							>
								<span
									aria-hidden="true"
									className={cn(
										"absolute top-2 left-0 h-[calc(100%-1rem)] w-px transition-colors",
										isActive ? "bg-primary" : "bg-transparent"
									)}
								/>
								<span
									className={cn(
										"font-mono text-[10px] tabular-nums",
										isActive ? "text-primary" : "text-muted-foreground/70"
									)}
								>
									{String(index + 1).padStart(2, "0")}
								</span>
								<span className="flex-1">{item.title}</span>
							</a>
						</li>
					);
				})}
			</ol>
		</nav>
	);
}
