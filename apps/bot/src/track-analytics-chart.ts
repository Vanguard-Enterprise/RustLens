import { createCanvas } from "@napi-rs/canvas";
import type { TrackGroupAnalyticsResponse } from "./backend-client.js";

const WIDTH = 1280;
const HEIGHT = 1080;
const PADDING = 54;
const HEATMAP_LEFT = 118;
const HEATMAP_TOP = 116;
const HEATMAP_WIDTH = 1015;
const HEATMAP_HEIGHT = 520;
const BAR_LEFT = 118;
const BAR_TOP = 740;
const BAR_WIDTH = 1015;
const BAR_HEIGHT = 270;
const WEEKDAY_LABELS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function cellColor(minutes: number) {
  const t = clamp(minutes / 60, 0, 1);
  const stops = [
    [20, 25, 38],
    [41, 75, 120],
    [56, 137, 128],
    [222, 129, 29],
    [250, 214, 120],
  ];
  const scaled = t * (stops.length - 1);
  const index = Math.min(stops.length - 2, Math.floor(scaled));
  const local = scaled - index;
  const from = stops[index];
  const to = stops[index + 1];
  const rgb = from.map((part, channel) => Math.round(part + (to[channel] - part) * local));
  return `rgb(${rgb[0]}, ${rgb[1]}, ${rgb[2]})`;
}

function getHourMinutes(analytics: TrackGroupAnalyticsResponse, weekdayIndex: number, hour: number) {
  const day = analytics.days[weekdayIndex];
  const entry = day?.hours.find((item) => (item.hour ?? new Date(item.hourUnix * 1000).getUTCHours()) === hour);
  return clamp(entry?.minutes ?? entry?.avg ?? 0, 0, 60);
}

function averageHourlyMinutes(analytics: TrackGroupAnalyticsResponse) {
  return Array.from({ length: 24 }, (_value, hour) => {
    const values = analytics.days.map((_day, weekday) => getHourMinutes(analytics, weekday, hour));
    return values.reduce((sum, value) => sum + value, 0) / Math.max(1, values.length);
  });
}

export async function renderTrackAnalyticsChart(analytics: TrackGroupAnalyticsResponse) {
  const canvas = createCanvas(WIDTH, HEIGHT);
  const ctx = canvas.getContext("2d");
  const background = ctx.createLinearGradient(0, 0, WIDTH, HEIGHT);
  background.addColorStop(0, "#090d15");
  background.addColorStop(1, "#121827");
  ctx.fillStyle = background;
  ctx.fillRect(0, 0, WIDTH, HEIGHT);

  ctx.fillStyle = "#f4f7fb";
  ctx.font = "700 34px Arial";
  ctx.fillText(`Analytics - ${analytics.groupName}`, PADDING, 56);
  ctx.fillStyle = "#9aa8bd";
  ctx.font = "18px Arial";
  ctx.fillText(`${analytics.serverName} | Minutes played per qualifying player-day | ${analytics.timeZone || "Europe/London"}`, PADDING, 86);

  drawHeatmap(ctx, analytics);
  drawBars(ctx, analytics);
  drawLegend(ctx);

  return canvas.encode("png");
}

function drawHeatmap(ctx: any, analytics: TrackGroupAnalyticsResponse) {
  ctx.fillStyle = "#101723";
  roundedRect(ctx, HEATMAP_LEFT - 60, HEATMAP_TOP - 52, HEATMAP_WIDTH + 118, HEATMAP_HEIGHT + 88, 22);
  ctx.fill();

  ctx.fillStyle = "#e8eef7";
  ctx.font = "700 22px Arial";
  ctx.fillText("Weekday / Hour Heatmap", HEATMAP_LEFT - 42, HEATMAP_TOP - 18);

  const cellW = HEATMAP_WIDTH / 7;
  const cellH = HEATMAP_HEIGHT / 24;

  ctx.textAlign = "center";
  ctx.fillStyle = "#bfcbda";
  ctx.font = "16px Arial";
  for (let day = 0; day < 7; day++) {
    ctx.fillText(WEEKDAY_LABELS[day], HEATMAP_LEFT + day * cellW + cellW / 2, HEATMAP_TOP - 14);
  }

  ctx.textAlign = "right";
  ctx.fillStyle = "#8795aa";
  ctx.font = "13px Arial";
  for (let hour = 0; hour < 24; hour++) {
    const y = HEATMAP_TOP + hour * cellH;
    ctx.fillText(String(hour).padStart(2, "0"), HEATMAP_LEFT - 12, y + cellH * 0.68);
    for (let day = 0; day < 7; day++) {
      const minutes = getHourMinutes(analytics, day, hour);
      ctx.fillStyle = cellColor(minutes);
      ctx.fillRect(HEATMAP_LEFT + day * cellW + 1, y + 1, cellW - 2, cellH - 2);
    }
  }

  ctx.strokeStyle = "#263143";
  ctx.lineWidth = 1;
  for (let day = 0; day <= 7; day++) {
    const x = HEATMAP_LEFT + day * cellW;
    ctx.beginPath();
    ctx.moveTo(x, HEATMAP_TOP);
    ctx.lineTo(x, HEATMAP_TOP + HEATMAP_HEIGHT);
    ctx.stroke();
  }
}

function drawBars(ctx: any, analytics: TrackGroupAnalyticsResponse) {
  ctx.fillStyle = "#101723";
  roundedRect(ctx, BAR_LEFT - 60, BAR_TOP - 54, BAR_WIDTH + 118, BAR_HEIGHT + 92, 22);
  ctx.fill();

  ctx.fillStyle = "#e8eef7";
  ctx.font = "700 22px Arial";
  ctx.fillText("Average By Hour", BAR_LEFT - 42, BAR_TOP - 20);

  const values = averageHourlyMinutes(analytics);
  const max = Math.max(10, Math.ceil(Math.max(...values) / 10) * 10);
  const rowH = BAR_HEIGHT / 24;

  ctx.strokeStyle = "#263143";
  ctx.fillStyle = "#8795aa";
  ctx.font = "13px Arial";
  ctx.textAlign = "center";
  for (let tick = 0; tick <= max; tick += Math.max(5, max / 4)) {
    const x = BAR_LEFT + (tick / max) * BAR_WIDTH;
    ctx.beginPath();
    ctx.moveTo(x, BAR_TOP);
    ctx.lineTo(x, BAR_TOP + BAR_HEIGHT);
    ctx.stroke();
    ctx.fillText(String(Math.round(tick)), x, BAR_TOP + BAR_HEIGHT + 22);
  }
  ctx.fillText("minutes", BAR_LEFT + BAR_WIDTH / 2, BAR_TOP + BAR_HEIGHT + 48);

  ctx.textAlign = "right";
  for (let hour = 0; hour < 24; hour++) {
    const y = BAR_TOP + hour * rowH;
    ctx.fillStyle = "#8795aa";
    ctx.fillText(String(hour).padStart(2, "0"), BAR_LEFT - 12, y + rowH * 0.74);
    const width = (values[hour] / max) * BAR_WIDTH;
    const gradient = ctx.createLinearGradient(BAR_LEFT, 0, BAR_LEFT + BAR_WIDTH, 0);
    gradient.addColorStop(0, "#2d789f");
    gradient.addColorStop(0.7, "#de811d");
    gradient.addColorStop(1, "#f6d57b");
    ctx.fillStyle = gradient;
    roundedRect(ctx, BAR_LEFT, y + 2, width, Math.max(2, rowH - 4), 4);
    ctx.fill();
  }
}

function drawLegend(ctx: any) {
  const x = HEATMAP_LEFT + HEATMAP_WIDTH + 24;
  const y = HEATMAP_TOP + 12;
  const w = 28;
  const h = 250;
  for (let i = 0; i < h; i++) {
    ctx.fillStyle = cellColor(60 - (i / h) * 60);
    ctx.fillRect(x, y + i, w, 1);
  }
  ctx.fillStyle = "#bfcbda";
  ctx.font = "13px Arial";
  ctx.textAlign = "left";
  ctx.fillText("60", x + w + 8, y + 5);
  ctx.fillText("30", x + w + 8, y + h / 2 + 5);
  ctx.fillText("0", x + w + 8, y + h + 5);
}

function roundedRect(ctx: any, x: number, y: number, w: number, h: number, r: number) {
  const radius = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + radius, y);
  ctx.arcTo(x + w, y, x + w, y + h, radius);
  ctx.arcTo(x + w, y + h, x, y + h, radius);
  ctx.arcTo(x, y + h, x, y, radius);
  ctx.arcTo(x, y, x + w, y, radius);
  ctx.closePath();
}
