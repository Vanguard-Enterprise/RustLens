import type { Metadata } from "next";
import { LegalPage } from "@/components/legal-page";
import { termsOfService } from "@/lib/legal-content";

export const metadata: Metadata = {
	title: "Terms of Service | RustLens",
	description: termsOfService.description,
};

export default function TermsPage() {
	return <LegalPage document={termsOfService} pathname="/terms" />;
}
