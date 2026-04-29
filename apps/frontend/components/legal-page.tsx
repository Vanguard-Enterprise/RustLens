import {
	ArrowUpRightIcon,
	CalendarDaysIcon,
	type LucideIcon,
	MailIcon,
} from "lucide-react";
import Link from "next/link";
import { Footer } from "@/components/footer";
import { Header } from "@/components/header";
import { LegalToc } from "@/components/legal-toc";
import { Button } from "@/components/ui/button";
import {
	type LegalDocument,
	legalContact,
	legalEffectiveDate,
	legalOperatorName,
} from "@/lib/legal-content";

type LegalPageProps = {
	document: LegalDocument;
	eyebrowIcon: LucideIcon;
	seeAlsoIcon: LucideIcon;
	alternate: {
		href: string;
		label: string;
	};
};

function slugify(value: string): string {
	return value
		.toLowerCase()
		.replace(/[^a-z0-9]+/g, "-")
		.replace(/^-+|-+$/g, "");
}

export function LegalPage({
	alternate,
	document,
	eyebrowIcon: EyebrowIcon,
	seeAlsoIcon: SeeAlsoIcon,
}: LegalPageProps) {
	const sections = document.sections.map((section) => ({
		...section,
		id: slugify(section.title),
	}));

	const tocItems = sections.map(({ id, title }) => ({ id, title }));

	return (
		<div className="flex min-h-screen flex-col">
			<Header />

			<main className="flex-1">
				<section className="mx-auto w-full max-w-5xl px-4 pt-14 pb-10 md:px-6 md:pt-20">
					<div className="flex flex-col gap-6">
						<div className="flex w-fit items-center gap-2 rounded-full border bg-card/80 px-3 py-1 shadow-xs backdrop-blur-sm">
							<EyebrowIcon className="size-3.5 text-primary" />
							<span className="font-medium text-foreground/80 text-xs uppercase tracking-[0.22em]">
								{legalOperatorName} legal
							</span>
						</div>

						<div className="space-y-5">
							<h1 className="font-heading font-semibold text-5xl text-foreground tracking-tight md:text-6xl">
								{document.title}
							</h1>
							<p className="max-w-3xl text-lg text-muted-foreground leading-relaxed md:text-xl">
								{document.summary}
							</p>
						</div>

						<dl className="mt-2 flex flex-wrap items-center gap-x-8 gap-y-3 border-border/70 border-y py-4 text-sm">
							<div className="flex items-center gap-2.5">
								<CalendarDaysIcon className="size-4 text-muted-foreground" />
								<dt className="text-muted-foreground">Effective date</dt>
								<dd className="font-medium text-foreground">
									{legalEffectiveDate}
								</dd>
							</div>
							<div className="flex items-center gap-2.5">
								<MailIcon className="size-4 text-muted-foreground" />
								<dt className="text-muted-foreground">Contact</dt>
								<dd className="font-mono text-foreground text-xs">
									{legalContact.email}
								</dd>
							</div>
							<div className="flex items-center gap-2.5">
								<SeeAlsoIcon className="size-4 text-muted-foreground" />
								<dt className="text-muted-foreground">See also</dt>
								<dd>
									<Link
										className="inline-flex items-center gap-1 font-medium text-primary hover:underline"
										href={alternate.href}
									>
										{alternate.label}
										<ArrowUpRightIcon className="size-3.5" />
									</Link>
								</dd>
							</div>
						</dl>
					</div>
				</section>

				<section className="mx-auto w-full max-w-5xl px-4 pb-20 md:px-6">
					<div className="grid gap-10 lg:grid-cols-[240px_minmax(0,1fr)] lg:gap-14">
						<aside className="lg:sticky lg:top-24 lg:self-start">
							<div className="rounded-2xl border bg-card/70 p-5 shadow-xs backdrop-blur-sm">
								<LegalToc items={tocItems} />
							</div>
						</aside>

						<div className="min-w-0 space-y-12">
							{sections.map((section, index) => (
								<article
									className="scroll-mt-24 space-y-5"
									id={section.id}
									key={section.id}
								>
									<header className="space-y-2">
										<div className="flex items-center gap-3">
											<span className="font-mono text-primary text-xs tabular-nums">
												{String(index + 1).padStart(2, "0")}
											</span>
											<span
												aria-hidden="true"
												className="h-px flex-1 bg-gradient-to-r from-border to-transparent"
											/>
										</div>
										<h2 className="font-heading font-semibold text-3xl text-foreground tracking-tight md:text-4xl">
											{section.title}
										</h2>
									</header>

									<div className="space-y-4 text-base text-foreground/80 leading-7 md:text-[1.05rem] md:leading-8">
										{section.paragraphs.map((paragraph) => (
											<p key={paragraph}>{paragraph}</p>
										))}

										{section.bullets ? (
											<ul className="mt-4 space-y-2.5 border-primary/30 border-l pl-5">
												{section.bullets.map((item) => (
													<li
														className="relative text-foreground/80 leading-7 before:-left-[1.4rem] before:absolute before:top-[0.65rem] before:size-1.5 before:rounded-full before:bg-primary/60"
														key={item}
													>
														{item}
													</li>
												))}
											</ul>
										) : null}
									</div>
								</article>
							))}

							<aside className="mt-16 overflow-hidden rounded-3xl border bg-card/80 p-8 shadow-sm backdrop-blur-sm md:p-10">
								<div className="flex flex-col gap-6 md:flex-row md:items-center md:justify-between">
									<div className="space-y-2">
										<p className="font-medium text-primary text-xs uppercase tracking-[0.22em]">
											Questions?
										</p>
										<h3 className="font-heading font-semibold text-2xl text-foreground tracking-tight md:text-3xl">
											Reach the {legalOperatorName} team
										</h3>
										<p className="max-w-lg text-muted-foreground text-sm leading-relaxed">
											{legalContact.responseNote}
										</p>
									</div>
									<div className="flex flex-col items-stretch gap-2 sm:flex-row md:flex-col md:items-end">
										<Button asChild>
											<a href={`mailto:${legalContact.email}`}>
												<MailIcon data-icon="inline-start" />
												{legalContact.email}
											</a>
										</Button>
										<Button asChild variant="outline">
											<Link href={alternate.href}>
												Read {alternate.label}
												<ArrowUpRightIcon data-icon="inline-end" />
											</Link>
										</Button>
									</div>
								</div>
							</aside>
						</div>
					</div>
				</section>
			</main>

			<Footer />
		</div>
	);
}
