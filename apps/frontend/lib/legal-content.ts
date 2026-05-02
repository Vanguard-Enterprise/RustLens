export type LegalSection = {
	title: string;
	paragraphs: string[];
	bullets?: string[];
};

export type LegalDocument = {
	title: string;
	description: string;
	summary: string;
	sections: LegalSection[];
};

export const legalOperatorName = "RustLens" as const;
export const legalEffectiveDate = "April 12, 2026" as const;
export const legalContact = {
	email: "contact@rustlens.com",
} as const;

export const termsOfService: LegalDocument = {
	title: "Terms of Service",
	description:
		"Rules, responsibilities, and service limitations for using RustLens across the website, dashboard, backend services, and Discord bot workflows.",
	summary:
		"These Terms govern access to RustLens, including the public website, authenticated dashboard, supporting backend services, and Discord bot features used by Rust communities.",
	sections: [
		{
			title: "Who RustLens is for",
			paragraphs: [
				"RustLens is a Discord-first operations service for Rust communities. It combines a website, authenticated dashboard, backend processing, and bot workflows for identity linking, tracked-server coordination, BattleMetrics visibility, wipe-react operations, and related guild administration.",
				"By accessing or using RustLens, you agree to use the service only for lawful community-management, moderation, and operational purposes. If you use RustLens on behalf of a guild, team, or organization, you represent that you have authority to act for that group.",
			],
		},
		{
			title: "Accounts and credential security",
			paragraphs: [
				"Some RustLens features require an account secured through Discord authentication. You are responsible for keeping your Discord account secure, for controlling access to your account, and for all activity that occurs under your session.",
				"You must provide accurate information when registering, linking accounts, or submitting Steam identifiers. You must promptly update information that becomes inaccurate and notify RustLens if you believe your account, guild configuration, or linked identities have been accessed without authorization.",
			],
			bullets: [
				"Do not share credentials or attempt to bypass authentication controls.",
				"Do not register accounts or connect identities using misleading, stolen, or unauthorized information.",
				"Do not use RustLens to impersonate another person, guild administrator, or trusted game identity.",
			],
		},
		{
			title: "Discord bot, linked identities, and community data",
			paragraphs: [
				"RustLens supports optional Discord account linking, Steam account association, BattleMetrics-based player matching, and guild-scoped identity workflows. You may only link or manage identities that you are authorized to control or administer.",
				"Guild administrators are responsible for how they configure logging channels, tracked servers, wipe-react schedules, nickname updates, and guild-scoped identity associations. RustLens may rely on data from Discord, Steam, and BattleMetrics to provide these features, and those external platforms remain responsible for their own services and data accuracy.",
			],
		},
		{
			title: "Acceptable use restrictions",
			paragraphs: [
				"You may not use RustLens in a manner that harms the service, other users, Discord communities, or third-party platforms that RustLens integrates with.",
			],
			bullets: [
				"Do not abuse, spam, scrape, overload, reverse engineer, or interfere with the service or its infrastructure.",
				"Do not use automation that circumvents intended rate limits, access controls, or guild-level permissions.",
				"Do not submit malicious content, attempt unauthorized data access, or use RustLens to support cheating, harassment, fraud, or unlawful conduct.",
			],
		},
		{
			title: "Third-party dependencies and service accuracy",
			paragraphs: [
				"RustLens depends on external services and data sources, including Discord, Steam, BattleMetrics, and hosting or infrastructure providers. RustLens does not guarantee that information from these dependencies is always complete, current, or error-free.",
				"Identity matching, player-presence reporting, guild membership state, tracked-server polling, and other operational outputs may lag, fail, or reflect stale third-party data. You are responsible for reviewing important operational decisions before acting on them.",
			],
		},
		{
			title: "Availability, warranties, and liability limits",
			paragraphs: [
				"RustLens is provided on an \"as is\" and \"as available\" basis. To the fullest extent allowed by law, RustLens disclaims warranties of merchantability, fitness for a particular purpose, non-infringement, and uninterrupted availability.",
				"RustLens is not liable for indirect, incidental, special, consequential, exemplary, or punitive damages, or for lost profits, lost data, guild disruption, moderation outcomes, or third-party platform actions arising from or related to your use of the service. If liability cannot be excluded, it is limited to the smallest amount permitted by applicable law.",
			],
		},
		{
			title: "Suspension and termination",
			paragraphs: [
				"RustLens may suspend, restrict, or terminate access to any part of the service if it reasonably believes you violated these Terms, created security or operational risk, misused linked identities, or used the service in a way that could harm RustLens, its users, or integrated platforms.",
				"You may stop using RustLens at any time. Manual data access, correction, or deletion requests can be submitted through the published RustLens legal contact once finalized.",
			],
		},
		{
			title: "Changes to these Terms",
			paragraphs: [
				"RustLens may update these Terms as the product, legal posture, or third-party integrations evolve. Material updates will be reflected by publishing a new effective date on this page, and continued use after an update means you accept the revised Terms.",
			],
		},
		{
			title: "Contact",
			paragraphs: [
				`For questions about these Terms, contact ${legalOperatorName} at ${legalContact.email}.`,
			],
		},
	],
};

export const refundPolicy: LegalDocument = {
	title: "Refund Policy",
	description:
		"All sales are final. RustLens does not offer refunds, return windows, or trial-period reversals on paid plans, add-ons, or one-time purchases.",
	summary:
		"All purchases of RustLens paid plans, add-ons, and one-time items are final. RustLens does not provide a return window or refunds, except where required by applicable law.",
	sections: [
		{
			title: "All sales are final",
			paragraphs: [
				"All purchases of paid RustLens products, including subscription plans, add-on features, and any one-time purchases, are final at the time the payment is completed. RustLens does not offer a return window, evaluation window, cooling-off period, or pro-rated refund for unused time on any plan.",
				"By completing a purchase, you acknowledge that you have reviewed the product description and pricing and agree that the transaction is non-refundable.",
			],
		},
		{
			title: "No refunds for unused or partial periods",
			paragraphs: [
				"RustLens does not refund unused time on a subscription if you cancel mid-cycle, stop using the service, remove the bot from your guild, lose admin authority over a guild, or otherwise no longer want the paid features. Access to paid features remains available for the period that has already been paid for, and the subscription will not renew once cancellation takes effect.",
			],
			bullets: [
				"No refunds for partially used billing cycles after cancellation.",
				"No refunds for renewals that proceed automatically on the scheduled billing date.",
				"No refunds for add-ons or one-time purchases once they have been delivered or activated.",
				"No refunds based on changes in your guild size, moderation needs, or feature preferences.",
			],
		},
		{
			title: "Statutory rights",
			paragraphs: [
				"Where mandatory local consumer law grants you a non-waivable right to a refund, withdrawal, or cancellation that overrides this policy, RustLens will honor those statutory rights to the extent legally required. This policy does not attempt to limit any right that cannot be excluded by contract.",
				"If you believe a non-waivable statutory right applies to your purchase, contact RustLens with the relevant details and RustLens will review the request under the applicable law.",
			],
		},
		{
			title: "Service failures and billing errors",
			paragraphs: [
				"If RustLens is unable to deliver a paid feature you purchased, or if you were charged in error (for example, a duplicate charge or a charge after a confirmed cancellation), contact RustLens. Confirmed billing errors and confirmed undelivered purchases will be corrected, typically by reversing the affected charge.",
				"Routine outages, third-party platform incidents (such as Discord, Steam, or BattleMetrics interruptions), and temporary degradations that are resolved within a reasonable time do not qualify as a service failure for refund purposes.",
			],
		},
		{
			title: "Cancellations",
			paragraphs: [
				"You may cancel a subscription at any time to prevent future renewals. Cancellation stops further billing on the next renewal date but does not refund the current period. Paid features remain available until the end of the period that has already been paid for, after which the account returns to free-tier access.",
			],
		},
		{
			title: "Chargebacks and disputes",
			paragraphs: [
				"Please contact RustLens before filing a payment dispute or chargeback. Most billing questions can be resolved more quickly through direct support.",
				"Filing a chargeback in circumstances that this policy does not cover may result in suspension of paid features and, where the dispute is determined to be invalid, may make future paid use of RustLens ineligible, except where required by law.",
			],
		},
		{
			title: "Changes to this Policy",
			paragraphs: [
				"RustLens may update this Refund Policy as paid features, billing arrangements, or legal requirements evolve. Updates take effect on the date noted on this page and apply to purchases made on or after that date. Purchases completed before an update remain governed by the policy that was in effect at the time of purchase.",
			],
		},
		{
			title: "Contact",
			paragraphs: [
				`For billing questions or statutory-rights claims, contact ${legalOperatorName} at ${legalContact.email}.`,
			],
		},
	],
};

export const privacyPolicy: LegalDocument = {
	title: "Privacy Policy",
	description:
		"How RustLens collects, uses, stores, and discloses account, identity-linking, guild, and operational data across the website, dashboard, backend, and Discord bot.",
	summary:
		"This Privacy Policy explains how RustLens handles personal and operational data when you use the website, authenticated dashboard, backend services, and Discord bot features.",
	sections: [
		{
			title: "Information RustLens collects",
			paragraphs: [
				"RustLens collects information needed to operate account access, Discord-integrated workflows, guild administration, tracked-server monitoring, and service security.",
			],
			bullets: [
				"Account and profile data, such as display name, avatar URL, account roles, and login timestamps.",
				"Discord account data, such as Discord user ID, username, display name, avatar, and guild membership state.",
				"Steam and BattleMetrics-related data, such as Steam IDs, BattleMetrics player IDs, BattleMetrics display names, avatars, soft or hard account links, and guild-scoped identity associations.",
				"Guild and operational data, such as guild settings, admin role assignments, tracked-server selections, wipe-react records and schedules, and player presence state synced from tracked servers.",
				"Security and audit data, such as auth events, administrative actions, request context needed for abuse prevention, and operational logging.",
			],
		},
		{
			title: "Where the information comes from",
			paragraphs: [
				"RustLens collects information directly from you when you sign in with Discord, link an account, or submit a Steam identifier. RustLens also receives information from Discord when OAuth or linked-account flows are used.",
				"RustLens may derive or refresh identity and activity information from external services such as BattleMetrics and Steam, and from bot-observed guild activity such as membership changes, guild-scoped linking actions, wipe-react participation, and tracked-server coordination events.",
			],
		},
		{
			title: "How RustLens uses the information",
			paragraphs: [
				"RustLens uses collected data to provide the service, maintain account security, support guild operations, and improve reliability of Discord and backend workflows.",
			],
			bullets: [
				"Authenticate users via Discord and manage sessions.",
				"Link or reconcile Discord, Steam, and BattleMetrics identities across user, guild, and bot workflows.",
				"Support guild administration, tracked-server management, wipe-react coordination, nickname targeting, and online-summary features.",
				"Operate, secure, debug, and audit the service, including abuse prevention and internal administrative review.",
				"Respond to support, legal, or policy inquiries and enforce RustLens terms and platform safety rules.",
			],
		},
		{
			title: "Sharing and disclosure",
			paragraphs: [
				"RustLens does not describe the current product as an advertising-supported service and does not position collected data for ad-tech resale. RustLens may disclose information only when reasonably necessary to operate the service, comply with law, protect rights or safety, or work with infrastructure and service providers acting on RustLens's behalf.",
				"RustLens may also disclose limited information to integrated platforms when a feature inherently depends on that platform, such as authenticating with Discord or requesting data from BattleMetrics or Steam-related services. Those third parties process data under their own terms and privacy policies.",
			],
		},
		{
			title: "Retention",
			paragraphs: [
				"RustLens retains data for as long as reasonably needed to operate the service, maintain account integrity, support guild workflows, investigate abuse, meet security needs, and satisfy legal obligations. Retention periods may differ by data type depending on operational necessity and risk.",
				"RustLens may delete, de-identify, or aggregate information when it is no longer reasonably necessary for these purposes. Manual deletion or correction requests will be reviewed through the published RustLens contact workflow once final contact details are available.",
			],
		},
		{
			title: "Your choices and requests",
			paragraphs: [
				"You may request access to, correction of, or deletion of certain personal information by contacting RustLens through its published legal or support contact. RustLens will review requests in light of account security, technical feasibility, operational records, and any legal obligations to retain information.",
				"RustLens does not currently promise self-service export or deletion tooling. Some information may need to be retained to preserve security logs, guild administration history, fraud prevention, or other legitimate operational records.",
			],
		},
		{
			title: "Children and age-appropriate use",
			paragraphs: [
				"RustLens is intended for users and communities that are old enough to use Discord and related services in their jurisdiction. RustLens is not directed to young children, and if RustLens becomes aware that personal information was collected from someone who is not permitted to use the service, RustLens may take steps to delete or restrict that data as appropriate.",
			],
		},
		{
			title: "Changes to this Policy",
			paragraphs: [
				"RustLens may update this Privacy Policy as the product, integrations, operational practices, or legal requirements change. When updates are made, RustLens will revise the effective date on this page and apply the updated policy going forward.",
			],
		},
		{
			title: "Contact",
			paragraphs: [
				`For privacy questions or data requests, contact ${legalOperatorName} at ${legalContact.email}.`,
			],
		},
	],
};
