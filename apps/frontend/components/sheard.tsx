import { cn } from "@/lib/utils";
import type React from "react";

export type LinkItemType = {
	label: string;
	href: string;
	icon: React.ReactNode;
	description?: string;
	blurred?: boolean;
	badge?: string;
};

export function LinkItem({
	label,
	description,
	icon,
	className,
	href,
	blurred,
	badge,
	...props
}: React.ComponentProps<"a"> & LinkItemType) {
	return (
		<a
			className={cn(
				"flex items-center gap-x-2",
				blurred && "pointer-events-none select-none",
				className
			)}
			href={blurred ? undefined : href}
			aria-disabled={blurred || undefined}
			tabIndex={blurred ? -1 : undefined}
			{...props}
		>
			<div
				className={cn(
					"flex aspect-square size-12 items-center justify-center rounded-md border bg-card text-sm shadow-sm",
					"[&_svg:not([class*='size-'])]:size-5 [&_svg:not([class*='size-'])]:text-foreground",
					blurred && "[&_svg]:blur-[3px] [&_svg]:opacity-70"
				)}
			>
				{icon}
			</div>
			<div className="flex flex-col items-start justify-center">
				<span className="flex items-center gap-2 font-medium">
					<span
						className={cn(blurred && "blur-[4px] select-none")}
						aria-hidden={blurred || undefined}
					>
						{blurred ? "Lorem Ipsum Dolor" : label}
					</span>
					{blurred ? <span className="sr-only">{label}</span> : null}
					{badge ? (
						<span className="rounded-full border bg-muted px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground whitespace-nowrap">
							{badge}
						</span>
					) : null}
				</span>
				<span
					className={cn(
						"line-clamp-2 text-muted-foreground text-xs",
						blurred && "blur-[3px] select-none"
					)}
					aria-hidden={blurred || undefined}
				>
					{blurred
						? "Sit amet consectetur adipiscing elit sed do"
						: description}
				</span>
				{blurred && description ? (
					<span className="sr-only">{description}</span>
				) : null}
			</div>
		</a>
	);
}
