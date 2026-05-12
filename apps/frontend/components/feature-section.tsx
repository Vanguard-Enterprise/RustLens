import { landingFeatures } from "@/lib/site-content";

export function FeatureSection() {
	return (
		<section className="mx-auto w-full max-w-5xl px-4 py-8 md:px-6">
			<div className="grid grid-cols-2 gap-4 md:grid-cols-4">
				{landingFeatures.map((feature) => {
					const Icon = feature.icon;

					return (
						<div
							className="flex flex-col items-center justify-center p-2 [&_svg]:size-6 [&_svg]:text-muted-foreground"
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
