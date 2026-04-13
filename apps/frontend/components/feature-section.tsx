import { cn } from "@/lib/utils";
import { landingFeatures } from "@/lib/site-content";

export function FeatureSection() {
	return (
		<section className="mx-auto w-full max-w-5xl px-4 py-8 md:px-6">
			<div className="grid grid-cols-2 gap-4 md:grid-cols-4">
				{landingFeatures.map((feature, index) => {
					const Icon = feature.icon;

					return (
						<div
							className={cn(
								"relative flex flex-col items-center justify-center p-2",
								"after:absolute after:inset-y-0 after:right-0 after:h-full after:w-px after:bg-linear-to-b after:from-foreground/6 after:via-foreground/25 after:to-foreground/6",
								"[&_svg]:size-6 [&_svg]:text-muted-foreground",
								{
									"after:hidden": index === landingFeatures.length - 1,
									"after:hidden after:md:block": index === 1,
								}
							)}
							key={feature.title}
						>
							<Icon />
							<h3 className="mt-4 text-center font-medium text-xs md:text-sm lg:text-base">
								{feature.title}
							</h3>
							<p className="mt-1 text-center text-[10px] text-muted-foreground md:text-xs">
								{feature.description}
							</p>
						</div>
					);
				})}
			</div>
		</section>
	);
}
