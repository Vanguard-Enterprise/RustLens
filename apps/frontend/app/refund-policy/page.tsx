import { RotateCcwIcon, ScaleIcon } from "lucide-react";
import type { Metadata } from "next";
import { LegalPage } from "@/components/legal-page";
import { refundPolicy } from "@/lib/legal-content";

export const metadata: Metadata = {
	title: refundPolicy.title,
	description: refundPolicy.description,
};

export default function RefundPolicyPage() {
	return (
		<LegalPage
			alternate={{ href: "/terms-of-service", label: "Terms of Service" }}
			document={refundPolicy}
			eyebrowIcon={RotateCcwIcon}
			seeAlsoIcon={ScaleIcon}
		/>
	);
}
