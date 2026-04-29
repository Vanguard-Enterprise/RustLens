"use client";
import { cn } from "@/lib/utils";
import NumberFlow from "@number-flow/react";
import { AnimatePresence, motion } from "motion/react";
import Link from "next/link";
import React from "react";
import { Button } from "@/components/ui/button";
import { type FREQUENCY, FrequencyToggle } from "@/components/frequency-toggle";
import { StarIcon, CheckCircleIcon } from "lucide-react";

type Plan = {
	name: string;
	info: string;
	price: {
		monthly: number;
		yearly: number; // yearly per month
	};
	features: string[];
	btn: {
		text: string;
		href: string;
	};
	highlighted?: boolean;
};

const plans: Plan[] = [
	{
		name: "Free",
		info: "For new clans getting set up",
		price: {
			monthly: 0,
			yearly: 0,
		},
		features: [
			"1 Discord guild",
			"Up to 5 tracked Rust servers",
			"Steam to Discord identity linking",
			"Live in-game presence in chat",
			"Community support",
		],
		btn: {
			text: "Get started",
			href: "/login",
		},
	},
	{
		highlighted: true,
		name: "Clan",
		info: "For active Rust communities",
		price: {
			monthly: 9,
			yearly: 7,
		},
		features: [
			"1 Discord guild",
			"Unlimited tracked Rust servers",
			"Wipe-react schedules and reactions",
			"BattleMetrics player matching",
			"Online roster summaries",
			"Nickname automation",
			"Priority support",
		],
		btn: {
			text: "Choose Clan",
			href: "/login",
		},
	},
	{
		name: "Network",
		info: "For multi-guild clan networks",
		price: {
			monthly: 29,
			yearly: 24,
		},
		features: [
			"Up to 10 Discord guilds",
			"Everything in Clan",
			"Cross-guild identity reconciliation",
			"Custom polling tiers",
			"Dedicated support",
			"Early access to new features",
		],
		btn: {
			text: "Contact us",
			href: "mailto:contact@rustlens.com",
		},
	},
];

export function PricingSection() {
	const [frequency, setFrequency] = React.useState<"monthly" | "yearly">(
		"monthly"
	);

	return (
		<div className="flex w-full flex-col items-center justify-center space-y-7 p-4">
			<div className="mx-auto max-w-xl space-y-2">
				<h2 className="text-center font-bold text-2xl tracking-tight md:text-3xl lg:font-extrabold lg:text-4xl">
					Pricing that scales with your community
				</h2>
				<p className="text-center text-muted-foreground text-sm md:text-base">
					Start free for your guild and upgrade when your clan grows. No hidden
					fees, no setup costs.
				</p>
			</div>

			<FrequencyToggle frequency={frequency} setFrequency={setFrequency} />
			<div className="mx-auto grid w-full max-w-4xl grid-cols-1 gap-6 md:grid-cols-3">
				{plans.map((plan) => (
					<PricingCard frequency={frequency} key={plan.name} plan={plan} />
				))}
			</div>
		</div>
	);
}

type PricingCardProps = React.ComponentProps<"div"> & {
	plan: Plan;
	frequency?: FREQUENCY;
};

export function PricingCard({
	plan,
	className,
	frequency = "monthly",
	...props
}: PricingCardProps) {
	return (
		<div
			className={cn(
				"relative flex w-full flex-col overflow-hidden rounded-lg border shadow-xs",
				plan.highlighted &&
					"scale-105 border-primary/50 shadow-md ring-1 ring-primary/20",
				className
			)}
			key={plan.name}
			{...props}
		>
			<div className="border-b p-4">
				<AnimatePresence mode="wait">
					<div className="absolute top-2 right-2 z-10 flex items-center gap-2">
						{plan.highlighted && (
							<motion.div
								className="flex items-center gap-1 rounded-md border bg-background px-2 py-0.5 text-xs"
								key="popular-badge"
								layout
								transition={{ duration: 0.1 }}
							>
								<StarIcon className="size-3 fill-current" />
								Popular
							</motion.div>
						)}

						{frequency === "yearly" &&
							plan.price.monthly > plan.price.yearly && (
								<motion.div
									animate={{ opacity: 1 }}
									className="flex items-center gap-1 rounded-md border bg-primary px-2 py-0.5 text-primary-foreground text-xs"
									exit={{ opacity: 0 }}
									initial={{ opacity: 0 }}
									key="discount-badge"
									layout
									transition={{ duration: 0.15 }}
								>
									{/* Calculate the actual discount percentage of the plan */}
									{Math.round(
										((plan.price.monthly - plan.price.yearly) /
											plan.price.monthly) *
											100
									)}
									% off
								</motion.div>
							)}
					</div>
				</AnimatePresence>

				<div className="font-medium text-lg">{plan.name}</div>
				<p className="font-normal text-muted-foreground text-sm">{plan.info}</p>
				<h3 className="mt-6 mb-1 flex w-max items-end gap-1">
					<NumberFlow
						className="font-extrabold text-3xl [&::part(suffix)]:font-normal [&::part(suffix)]:text-base [&::part(suffix)]:text-muted-foreground"
						format={{
							style: "currency",
							currency: "USD",
							notation: "compact",
						}}
						suffix="/month"
						value={plan.price[frequency]}
					/>
				</h3>
				<p className="mb-2 font-normal text-muted-foreground text-xs">
					billed {frequency}
				</p>
			</div>
			<div className="space-y-3 px-4 pt-6 pb-8 text-muted-foreground text-sm">
				{plan.features.map((feature) => (
					<div className="flex items-center gap-2" key={feature}>
						<CheckCircleIcon className="size-3.5 text-foreground" />
						<p>{feature}</p>
					</div>
				))}
			</div>
			<div className="mt-auto w-full border-t p-3">
				<Button
					asChild
					className="w-full"
					variant={plan.highlighted ? "default" : "outline"}
				>
					<Link href={plan.btn.href}>{plan.btn.text}</Link>
				</Button>
			</div>
		</div>
	);
}
