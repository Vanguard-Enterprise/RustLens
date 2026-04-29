import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

export function LinksSkeleton() {
	return (
		<Card>
			<CardHeader>
				<CardTitle>Your linked Steam accounts</CardTitle>
			</CardHeader>
			<CardContent className="flex flex-col gap-3">
				{Array.from({ length: 4 }).map((_, i) => (
					<div key={i} className="flex items-center justify-between gap-4">
						<Skeleton className="h-4 w-48" />
						<Skeleton className="h-4 w-24" />
					</div>
				))}
			</CardContent>
		</Card>
	);
}
