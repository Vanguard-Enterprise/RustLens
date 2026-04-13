import { FaqsSection } from "@/components/faqs-section";
import { FeatureSection } from "@/components/feature-section";
import { Footer } from "@/components/footer";
import { Header } from "@/components/header";
import { HeroSection } from "@/components/hero";
import { LogosSection } from "@/components/logos-section";

export default function Home() {
	return (
		<div className="flex min-h-screen flex-col">
			<Header />
			<main className="flex-1">
				<HeroSection />
				<LogosSection />
				<FeatureSection />
				<FaqsSection />
			</main>
			<Footer />
		</div>
	);
}
