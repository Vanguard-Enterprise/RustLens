import { ScaleIcon, ShieldCheckIcon } from "lucide-react";
import type { Metadata } from "next";
import { LegalPage } from "@/components/legal-page";
import { privacyPolicy } from "@/lib/legal-content";

export const metadata: Metadata = {
	title: privacyPolicy.title,
	description: privacyPolicy.description,
};

export default function PrivacyPolicyPage() {
	return (
		<LegalPage
			alternate={{ href: "/terms-of-service", label: "Terms of Service" }}
			document={privacyPolicy}
			eyebrowIcon={ShieldCheckIcon}
			seeAlsoIcon={ScaleIcon}
		/>
	);
}
