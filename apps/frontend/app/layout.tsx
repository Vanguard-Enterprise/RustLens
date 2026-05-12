import type { Metadata } from "next";
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
			suppressHydrationWarning
		>
			<body className="min-h-full flex flex-col">
				<script
					// biome-ignore lint/security/noDangerouslySetInnerHtml: inline guard runs before hydration
					dangerouslySetInnerHTML={{
						__html: `(()=>{try{var n=performance.getEntriesByType('navigation')[0];if(n&&n.type==='back_forward'&&!sessionStorage.getItem('__bfReloaded')){sessionStorage.setItem('__bfReloaded','1');location.reload();}else{sessionStorage.removeItem('__bfReloaded');}addEventListener('pageshow',function(e){if(e.persisted){location.reload();}});}catch(_){}})();`,
					}}
				/>
				<script
					// biome-ignore lint/security/noDangerouslySetInnerHtml: inline pointer tracker runs before hydration
					dangerouslySetInnerHTML={{
						__html: `(()=>{try{var r=document.documentElement;var x=-9999,y=-9999,t=0;r.style.setProperty('--mx','-9999px');r.style.setProperty('--my','-9999px');var f=function(){r.style.setProperty('--mx',x+'px');r.style.setProperty('--my',y+'px');t=0;};addEventListener('mousemove',function(e){x=e.clientX;y=e.clientY;if(!t)t=requestAnimationFrame(f);},{passive:true});document.addEventListener('mouseleave',function(){x=-9999;y=-9999;if(!t)t=requestAnimationFrame(f);});}catch(_){}})();`,
					}}
				/>
				<ThemeProvider
					attribute="class"
					defaultTheme="system"
					enableSystem
					disableTransitionOnChange
				>
					<TooltipProvider>{children}</TooltipProvider>
					<Toaster />
				</ThemeProvider>
			</body>
		</html>
	);
}
