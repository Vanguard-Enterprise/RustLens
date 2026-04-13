import Link from "next/link";
import { Footer } from "@/components/footer";
import { Header } from "@/components/header";
import {
	type LegalDocument,
	legalContact,
	legalEffectiveDate,
	legalOperatorName,
} from "@/lib/legal-content";

type LegalPageProps = {
	document: LegalDocument;
	pathname: "/privacy" | "/terms";
};

export function LegalPage({ document, pathname }: LegalPageProps) {
	const alternatePath = pathname === "/terms" ? "/privacy" : "/terms";
	const alternateLabel =
		pathname === "/terms" ? "Privacy Policy" : "Terms of Service";

	return (
		<div className="flex min-h-screen flex-col">
			<Header />
			<main className="flex-1">
				<section className="mx-auto flex w-full max-w-5xl flex-col gap-10 px-4 py-10 md:px-6 md:py-14">
					<div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_280px] lg:items-start">
						<div className="space-y-5">
							<p className="font-medium text-primary text-sm uppercase tracking-[0.24em]">
								{legalOperatorName} legal
							</p>
							<div className="space-y-4">
								<h1 className="font-semibold text-4xl tracking-tight md:text-5xl">
									{document.title}
								</h1>
								<p className="max-w-3xl text-lg text-muted-foreground">
									{document.summary}
								</p>
							</div>
						</div>

						<aside className="rounded-2xl border bg-background/80 p-5 backdrop-blur-sm">
							<div className="space-y-4 text-sm">
								<div>
									<p className="font-medium text-foreground">Effective date</p>
									<p className="mt-1 text-muted-foreground">
										{legalEffectiveDate}
									</p>
								</div>
								<div>
									<p className="font-medium text-foreground">Contact</p>
									<p className="mt-1 break-all text-muted-foreground">
										{legalContact.email}
									</p>
									<p className="mt-2 text-muted-foreground">
										{legalContact.responseNote}
									</p>
								</div>
								<div>
									<p className="font-medium text-foreground">Related page</p>
									<Link
										className="mt-1 inline-flex text-primary hover:underline"
										href={alternatePath}
									>
										Review the {alternateLabel}
									</Link>
								</div>
							</div>
						</aside>
					</div>

					<div className="rounded-[28px] border bg-background/85 p-6 shadow-sm backdrop-blur-sm md:p-10">
						<div className="space-y-10">
							{document.sections.map((section) => (
								<section className="space-y-4" key={section.title}>
									<h2 className="font-semibold text-2xl tracking-tight md:text-3xl">
										{section.title}
									</h2>
									<div className="space-y-4 text-base leading-7 text-muted-foreground">
										{section.paragraphs.map((paragraph) => (
											<p key={paragraph}>{paragraph}</p>
										))}
										{section.bullets ? (
											<ul className="space-y-3 pl-5">
												{section.bullets.map((item) => (
													<li className="list-disc" key={item}>
														{item}
													</li>
												))}
											</ul>
										) : null}
									</div>
								</section>
							))}
						</div>
					</div>
				</section>
			</main>
			<Footer />
		</div>
	);
}
