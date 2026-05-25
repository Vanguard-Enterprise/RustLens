import { FaqsSection } from "@/components/faqs-section";
import { FeatureSection } from "@/components/feature-section";
import { Footer } from "@/components/footer";
import { Header } from "@/components/header";
import { HeroSection } from "@/components/hero";
import { TestimonialsSection } from "@/components/testimonials-section";
import { MouseGridBackground } from "@/components/mouse-grid-background";
import { PricingSection } from "@/components/pricing-section";
import { getCsrfToken } from "@/lib/csrf";
import { getSession } from "@/lib/session";

export default async function Home() {
	const session = await getSession();
	const csrfToken = session ? await getCsrfToken() : undefined;
	const user = session
		? {
				displayName: session.displayName,
				avatarUrl: session.avatarUrl,
				isInternalAdmin:
					(session.adminRoles?.length ?? 0) > 0 ||
					session.roles.includes("internal_admin"),
			}
		: null;

	return (
		<div className="relative flex min-h-screen flex-col overflow-x-clip">
			<MouseGridBackground />
			<div className="relative z-10 flex min-h-screen flex-col">
				<Header csrfToken={csrfToken} user={user} />
				<main className="flex-1">
					<HeroSection />
					<TestimonialsSection />
					<FeatureSection />
					<PricingSection />
					<FaqsSection />
				</main>
				<Footer />
			</div>
		</div>
	);
}
