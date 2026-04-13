import type { Metadata } from "next";
import { LegalPage } from "@/components/legal-page";
import { privacyPolicy } from "@/lib/legal-content";

export const metadata: Metadata = {
	title: "Privacy Policy | RustLens",
	description: privacyPolicy.description,
};

export default function PrivacyPage() {
	return <LegalPage document={privacyPolicy} pathname="/privacy" />;
}
