"use client";

export function UsageSection() {
	return (
		<section className="flex flex-col gap-3 rounded-lg border bg-card p-4">
			<div>
				<h2 className="text-lg font-semibold">Usage</h2>
				<p className="text-sm text-muted-foreground">
					Commands this user has run and where they were run.
				</p>
			</div>
			<p className="text-sm text-muted-foreground">
				Usage tracking is not yet available.
			</p>
		</section>
	);
}
