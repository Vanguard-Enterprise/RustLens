"use client";

import Autoplay from "embla-carousel-autoplay";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import {
	Carousel,
	CarouselContent,
	CarouselItem,
	CarouselNext,
	CarouselPrevious,
} from "@/components/ui/carousel";

type Testimonial = {
	quote: string;
	name: string;
	role: string;
	avatar: string;
};

const testimonials: Testimonial[] = [
	{
		quote:
			"placeholder",
		name: "Fillee",
		role: "Founder, Cleanup Crew",
		avatar: "https://cdn.discordapp.com/avatars/365258431801851905/6d48bfe59b7c5d1741cb5b6b77729a74.webp?size=1024",
	},
	{
		quote:
			"The /analyze heatmaps changed how we raid. We can actually see when clans go offline instead of guessing.",
		name: "Rift",
		role: "Strategist, Obsidian",
		avatar: "",
	},
	{
		quote:
			"Tracking alt accounts across wipes was a nightmare. /checkgamebans caught three banned alts in our first week.",
		name: "Zoe",
		role: "Admin, Rusty Syndicate",
		avatar: "",
	},
	{
		quote:
			"Our members actually check #server-status now because the live player counts auto-update. Less pings in general chat.",
		name: "Hex",
		role: "Moderator, Iron Wolves",
		avatar: "",
	},
	{
		quote:
			"Setup took five minutes. The bot just works. No config rabbit holes, no weird permissions issues.",
		name: "Niko",
		role: "Founder, Deadzone",
		avatar: "",
	},
	{
		quote:
			"The /compare command is underrated. Being able to see who played on which servers across wipes is genuinely useful for recruitment.",
		name: "Mara",
		role: "Recruiter, Phantom Recon",
		avatar: "",
	},
];

export function TestimonialsSection() {
	return (
		<section className="mx-auto w-full max-w-5xl space-y-6 px-4 py-10 md:px-8">
			<div className="space-y-2 text-center">
				<h2 className="font-medium text-lg text-muted-foreground tracking-tight md:text-xl">
					Trusted by <span className="text-foreground">Clans</span>
				</h2>
			</div>

			<Carousel
				opts={{ align: "start", loop: true }}
				plugins={[Autoplay({ delay: 4000, stopOnInteraction: true })]}
				className="mx-auto w-full max-w-4xl"
			>
				<CarouselContent>
					{testimonials.map((t) => (
						<CarouselItem
							key={t.name}
							className="basis-full md:basis-1/2"
						>
							<div className="flex h-full flex-col justify-between gap-5 px-2 py-4">
								<p className="text-muted-foreground text-base leading-relaxed">
									&ldquo;{t.quote}&rdquo;
								</p>
								<div className="flex items-center gap-3">
									<Avatar>
										{t.avatar && (
											<AvatarImage src={t.avatar} alt={t.name} />
										)}
										<AvatarFallback>
											{t.name.slice(0, 2).toUpperCase()}
										</AvatarFallback>
									</Avatar>
									<div>
										<p className="font-medium text-sm leading-none">
											{t.name}
										</p>
										<p className="mt-1 text-muted-foreground text-xs">
											{t.role}
										</p>
									</div>
								</div>
							</div>
						</CarouselItem>
					))}
				</CarouselContent>
				<CarouselPrevious />
				<CarouselNext />
			</Carousel>

			<p className="text-center text-muted-foreground/60 text-xs">
				Want to be featured?{" "}
				<a
					href="https://rustlens.com/discord"
					className="underline hover:text-muted-foreground"
				>
					Open a ticket in our Discord
				</a>
			</p>
		</section>
	);
}
