import {
	Accordion,
	AccordionContent,
	AccordionItem,
	AccordionTrigger,
} from "@/components/ui/accordion";
import { faqs } from "@/lib/site-content";

export function FaqsSection() {
	return (
		<section className="mx-auto w-full max-w-2xl space-y-7 px-4 py-10">
			<div className="space-y-2">
				<h2 className="font-semibold text-3xl md:text-4xl">
					Frequently Asked Questions
				</h2>
				<p className="max-w-2xl text-muted-foreground">
					Common questions about how RustLens handles tracked servers,
					identity links, BattleMetrics visibility, and the current
					landing experience.
				</p>
			</div>
			<Accordion className="rounded-lg border" collapsible type="single">
				{faqs.map((item) => (
					<AccordionItem className="px-4" key={item.id} value={item.id}>
						<AccordionTrigger className="py-4 hover:no-underline focus-visible:underline focus-visible:ring-0">
							{item.title}
						</AccordionTrigger>
						<AccordionContent className="pb-4! text-muted-foreground">
							{item.content}
						</AccordionContent>
					</AccordionItem>
				))}
			</Accordion>
		</section>
	);
}
