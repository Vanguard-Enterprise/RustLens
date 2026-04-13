"use client";

import { Button } from "@/components/ui/button";
import {
	Empty,
	EmptyContent,
	EmptyHeader,
	EmptyTitle,
} from "@/components/ui/empty";
import { ArrowLeftIcon, HomeIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";

export function NotFoundPage() {
	const router = useRouter();

	return (
		<section className="mx-auto flex w-full max-w-5xl flex-1 items-center justify-center px-4 py-16 md:px-6 md:py-24">
			<Empty className="gap-6 px-6 py-10 md:px-10">
				<EmptyHeader className="max-w-2xl gap-3">
					<EmptyTitle className="mask-b-from-20% mask-b-to-80% font-heading font-extrabold text-8xl text-foreground/90 leading-none md:text-9xl">
						404
					</EmptyTitle>
					<EmptyTitle className="font-heading text-2xl text-foreground md:text-3xl">
						Route not found
					</EmptyTitle>
				</EmptyHeader>
				<EmptyContent className="max-w-none">
					<div className="flex flex-col items-center gap-3 sm:flex-row">
						<Button onClick={() => router.back()} type="button" variant="outline">
							<ArrowLeftIcon data-icon="inline-start" />
							Return to previous
						</Button>
						<Button asChild>
							<Link href="/">
								<HomeIcon data-icon="inline-start" />
								Go Home
							</Link>
						</Button>
					</div>
				</EmptyContent>
			</Empty>
		</section>
	);
}
