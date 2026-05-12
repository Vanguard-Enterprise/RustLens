import type { Metadata } from "next";
import { Fraunces, IBM_Plex_Mono, IBM_Plex_Sans } from "next/font/google";
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

export const metadata: Metadata = {
	title: "RustLens",
	description:
		"Discord-first operating layer for Rust communities managing tracked servers, BattleMetrics visibility, and wipe coordination.",
};

export default function RootLayout({
	children,
}: Readonly<{
	children: React.ReactNode;
}>) {
	return (
		<html
			className={`${bodyFont.variable} ${monoFont.variable} ${headingFont.variable} h-full antialiased`}
			lang="en"
		>
			<body
				className="min-h-full flex flex-col"
				style={{
					backgroundImage:
						"linear-gradient(to right, color-mix(in srgb, var(--border) 40%, transparent) 1px, transparent 1px), linear-gradient(to bottom, color-mix(in srgb, var(--border) 35%, transparent) 1px, transparent 1px)",
					backgroundSize: "36px 36px, 36px 36px",
				}}
			>
				<script
					// biome-ignore lint/security/noDangerouslySetInnerHtml: inline guard runs before hydration
					dangerouslySetInnerHTML={{
						__html: `(()=>{try{var n=performance.getEntriesByType('navigation')[0];if(n&&n.type==='back_forward'&&!sessionStorage.getItem('__bfReloaded')){sessionStorage.setItem('__bfReloaded','1');location.reload();}else{sessionStorage.removeItem('__bfReloaded');}addEventListener('pageshow',function(e){if(e.persisted){location.reload();}});}catch(_){}})();`,
					}}
				/>
				<TooltipProvider>{children}</TooltipProvider>
				<Toaster />
			</body>
		</html>
	);
}
