import { ImageResponse } from "next/og";

export const runtime = "edge";

export const alt = "RustLens - Discord Bot for Rust Community Management";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default async function Image() {
	return new ImageResponse(
		(
			<div
				style={{
					display: "flex",
					flexDirection: "column",
					alignItems: "center",
					justifyContent: "center",
					width: "100%",
					height: "100%",
					backgroundColor: "#0a0a0a",
					padding: "60px",
					gap: "32px",
				}}
			>
				<img
					src="https://rustlens.com/RustLens.png"
					alt=""
					width={140}
					height={140}
				/>
				<div
					style={{
						display: "flex",
						flexDirection: "column",
						alignItems: "center",
						gap: "16px",
					}}
				>
					<div
						style={{
							fontSize: 64,
							fontWeight: 700,
							color: "#ffffff",
							letterSpacing: "-1px",
						}}
					>
						RustLens
					</div>
					<div
						style={{
							fontSize: 28,
							color: "#a1a1a1",
							textAlign: "center",
							maxWidth: "800px",
							lineHeight: 1.4,
						}}
					>
						Discord bot for Rust communities. Track players, coordinate wipes, and manage servers.
					</div>
				</div>
				<div
					style={{
						display: "flex",
						alignItems: "center",
						gap: "8px",
						marginTop: "12px",
						padding: "10px 24px",
						borderRadius: "8px",
						border: "1px solid #de811d",
						color: "#de811d",
						fontSize: 20,
					}}
				>
					rustlens.com
				</div>
			</div>
		),
		{ ...size }
	);
}
