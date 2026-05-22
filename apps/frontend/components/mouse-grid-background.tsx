"use client";

import { useEffect, useRef } from "react";

const DOT_GAP = 64;
const DOT_SIZE = 1.2;
const MOUSE_RADIUS = 360;
const DARK_BASE_ALPHA = 0.055;
const DARK_PEAK_ALPHA = 0.35;
const LIGHT_BASE_ALPHA = 0.09;
const LIGHT_PEAK_ALPHA = 0.38;
const PUSH_FORCE = -1;
const LERP = 0.08;
const CANVAS_INSET = 20;

type ActiveDot = {
	alpha: number;
	column: number;
	row: number;
	x: number;
	y: number;
};

function getCenteredAxisPositions(length: number) {
	const dotCount = Math.max(2, Math.floor(length / DOT_GAP) + 1);
	const edgeGap = (length - (dotCount - 1) * DOT_GAP) / 2;
	return Array.from({ length: dotCount }, (_, index) => edgeGap + index * DOT_GAP);
}

export function MouseGridBackground() {
	const canvasRef = useRef<HTMLCanvasElement>(null);

	useEffect(() => {
		const canvas = canvasRef.current;
		if (!canvas) {
			return;
		}

		const canvasElement = canvas;
		const drawingContext = canvasElement.getContext("2d", { alpha: true });
		if (!drawingContext) {
			return;
		}
		const context = drawingContext;

		let width = 0;
		let height = 0;
		let mouseX = -1000;
		let mouseY = -1000;
		let targetMouseX = -1000;
		let targetMouseY = -1000;
		let animationFrame = 0;

		function resize() {
			const dpr = window.devicePixelRatio || 1;

			width = Math.max(0, window.innerWidth - CANVAS_INSET * 2);
			height = Math.max(
				0,
				document.documentElement.scrollHeight - CANVAS_INSET * 2,
				window.innerHeight - CANVAS_INSET * 2
			);

			canvasElement.style.width = `${width}px`;
			canvasElement.style.height = `${height}px`;

			canvasElement.width = Math.round(width * dpr);
			canvasElement.height = Math.round(height * dpr);
			context.setTransform(dpr, 0, 0, dpr, 0, 0);
		}

		function handlePointerMove(event: PointerEvent) {
			const rect = canvasElement.getBoundingClientRect();
			targetMouseX = event.clientX - rect.left;
			targetMouseY = event.clientY - rect.top;
		}

		function handlePointerLeave() {
			targetMouseX = -1000;
			targetMouseY = -1000;
		}

		function animate() {
			const isDark = document.documentElement.classList.contains("dark");
			const baseAlpha = isDark ? DARK_BASE_ALPHA : LIGHT_BASE_ALPHA;
			const peakAlpha = isDark ? DARK_PEAK_ALPHA : LIGHT_PEAK_ALPHA;
			const dotColor = isDark ? "180,180,180" : "92,92,92";
			const lineColor = isDark ? "222,222,222" : "82,82,82";

			mouseX += (targetMouseX - mouseX) * LERP;
			mouseY += (targetMouseY - mouseY) * LERP;

			context.clearRect(0, 0, width, height);

			const activeDots = new Map<string, ActiveDot>();
			const columns = getCenteredAxisPositions(width);
			const rows = getCenteredAxisPositions(height);

			for (const [columnIndex, x] of columns.entries()) {
				for (const [rowIndex, y] of rows.entries()) {
					const dx = x - mouseX;
					const dy = y - mouseY;
					const distance = Math.hypot(dx, dy);
					const strength = Math.max(0, 1 - distance / MOUSE_RADIUS);
					const power = strength * strength;
					const alpha = baseAlpha + power * (peakAlpha - baseAlpha);
					const pushX =
						distance > 0 ? power * (dx / distance) * PUSH_FORCE : 0;
					const pushY =
						distance > 0 ? power * (dy / distance) * PUSH_FORCE : 0;
					const finalX = x + pushX;
					const finalY = y + pushY;
					const size = DOT_SIZE + power * 1.3;

					context.fillStyle = `rgba(${dotColor},${alpha})`;
					context.fillRect(finalX - size / 2, finalY - size / 2, size, size);

					if (power > 0.08) {
						activeDots.set(`${columnIndex}:${rowIndex}`, {
							alpha: power,
							column: columnIndex,
							row: rowIndex,
							x: finalX,
							y: finalY,
						});
					}
				}
			}

			context.lineWidth = 0.5;
			for (const dot of activeDots.values()) {
				const right = activeDots.get(`${dot.column + 1}:${dot.row}`);
				const below = activeDots.get(`${dot.column}:${dot.row + 1}`);

				for (const neighbor of [right, below]) {
					if (!neighbor) {
						continue;
					}

					const alpha = Math.min(dot.alpha, neighbor.alpha) * 0.24;
					context.beginPath();
					context.moveTo(dot.x, dot.y);
					context.lineTo(neighbor.x, neighbor.y);
					context.strokeStyle = `rgba(${lineColor},${alpha})`;
					context.stroke();
				}
			}

			animationFrame = window.requestAnimationFrame(animate);
		}

		resize();
		animate();

		const resizeObserver = new ResizeObserver(resize);
		resizeObserver.observe(document.body);
		window.addEventListener("resize", resize);
		window.addEventListener("pointermove", handlePointerMove);
		window.addEventListener("pointerleave", handlePointerLeave);

		return () => {
			window.cancelAnimationFrame(animationFrame);
			resizeObserver.disconnect();
			window.removeEventListener("resize", resize);
			window.removeEventListener("pointermove", handlePointerMove);
			window.removeEventListener("pointerleave", handlePointerLeave);
		};
	}, []);

	return (
		<canvas
			aria-hidden="true"
			className="pointer-events-none absolute top-5 left-5 z-0 block opacity-80 dark:opacity-70"
			ref={canvasRef}
		/>
	);
}
