import type { Metadata } from "next";
import { headers } from "next/headers";
import { Fraunces, IBM_Plex_Mono, IBM_Plex_Sans } from "next/font/google";
import { ThemeProvider } from "next-themes";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import "./globals.css";

const bodyFont = IBM_Plex_Sans({
	variable: "--font-body",
	subsets: ["latin"],
	weight: ["400", "500", "600"],
});

const monoFont = IBM_Plex_Mono({
	variable: "--font-mono",
	subsets: ["latin"],
	weight: ["400", "500"],
});

const headingFont = Fraunces({
	variable: "--font-heading",
	subsets: ["latin"],
	weight: ["500", "600", "700"],
});

export const metadataBase = new URL("https://rustlens.com");

export const metadata: Metadata = {
	title: "RustLens",
	description:
		"Discord-first operating layer for Rust communities managing tracked servers, BattleMetrics visibility, and wipe coordination.",
	themeColor: "#de811d",
	openGraph: {
		siteName: "RustLens",
		type: "website",
		locale: "en_US",
	},
	twitter: {
		card: "summary_large_image",
	},
};

export default async function RootLayout({
	children,
}: Readonly<{
	children: React.ReactNode;
}>) {
	// Read the per-request CSP nonce injected by middleware.ts. Both inline
	// scripts below must carry it so they execute under 'strict-dynamic'.
	const nonce = (await headers()).get("x-nonce") ?? undefined;
	return (
		<html
			className={`${bodyFont.variable} ${monoFont.variable} ${headingFont.variable} h-full antialiased`}
			lang="en"
			suppressHydrationWarning
		>
			<body className="min-h-full flex flex-col">
				<script
					nonce={nonce}
					suppressHydrationWarning
					// biome-ignore lint/security/noDangerouslySetInnerHtml: inline guard runs before hydration
					dangerouslySetInnerHTML={{
						__html: `(()=>{try{var n=performance.getEntriesByType('navigation')[0];if(n&&n.type==='back_forward'&&!sessionStorage.getItem('__bfReloaded')){sessionStorage.setItem('__bfReloaded','1');location.reload();}else{sessionStorage.removeItem('__bfReloaded');}addEventListener('pageshow',function(e){if(e.persisted){location.reload();}});}catch(_){}})();`,
					}}
				/>
				<ThemeProvider
					attribute="class"
					defaultTheme="system"
					enableSystem
					disableTransitionOnChange
					nonce={nonce}
				>
					<TooltipProvider>{children}</TooltipProvider>
					<Toaster />
				</ThemeProvider>
			</body>
		</html>
	);
}
