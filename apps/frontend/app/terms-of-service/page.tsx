import { ScaleIcon, ShieldCheckIcon } from "lucide-react";
import type { Metadata } from "next";
import { LegalPage } from "@/components/legal-page";
import { termsOfService } from "@/lib/legal-content";

export const metadata: Metadata = {
	title: termsOfService.title,
	description: termsOfService.description,
};

export default function TermsOfServicePage() {
	return (
		<LegalPage
			alternate={{ href: "/privacy", label: "Privacy Policy" }}
			document={termsOfService}
			eyebrowIcon={ScaleIcon}
			seeAlsoIcon={ShieldCheckIcon}
		/>
	);
}
