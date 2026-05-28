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
			"The /wipereact schedule command was a huge time saver. Instead of manually setting up a wipe react before every wipe, a single command handled everything automatically.",
		name: "Eonstriker",
		role: "Support & Early Adopter, RustLens",
		avatar: "https://cdn.discordapp.com/avatars/701201435030716477/c736c6c175ec6a8714140fedb1b8c0e6.webp?size=1024",
	},
	{
		quote:
			"Finding a specific player used to mean scrolling BattleMetrics for ages. Now /search pulls them up in a moment, online or off. I love Rustlens",
		name: "Jinx",
		role: "Early Adopter, RustLens",
		avatar: "https://cdn.discordapp.com/avatars/741464240099033118/aee0a09fb6f09fc3941cd8d0e1fa6087.webp?size=1024",
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
