import { createCanvas } from "@napi-rs/canvas";
import type { TrackGroupAnalyticsResponse } from "./backend-client.js";

const WIDTH = 1600;
const HEIGHT = 900;
const PADDING = 64;
const HEATMAP_LEFT = 112;
const HEATMAP_TOP = 150;
const HEATMAP_CELL = 48;
const HEATMAP_WIDTH = HEATMAP_CELL * 24;
const HEATMAP_HEIGHT = HEATMAP_CELL * 7;
const BAR_LEFT = 112;
const BAR_TOP = 615;
const BAR_WIDTH = 1376;
const BAR_HEIGHT = 215;
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
  ctx.fillStyle = "#e8eef7";
  ctx.font = "700 22px Arial";
  ctx.textAlign = "left";
  ctx.fillText("Weekday / Hour Heatmap", HEATMAP_LEFT, HEATMAP_TOP - 58);

  ctx.textAlign = "center";
  ctx.fillStyle = "#bfcbda";
  ctx.font = "13px Arial";
  for (let hour = 0; hour < 24; hour += 2) {
    ctx.fillText(String(hour).padStart(2, "0"), HEATMAP_LEFT + hour * HEATMAP_CELL + HEATMAP_CELL / 2, HEATMAP_TOP - 18);
  }
  ctx.font = "12px Arial";
  ctx.fillText("hour", HEATMAP_LEFT + HEATMAP_WIDTH / 2, HEATMAP_TOP - 38);

  ctx.textAlign = "right";
  ctx.fillStyle = "#8795aa";
  ctx.font = "15px Arial";
  for (let day = 0; day < 7; day++) {
    const y = HEATMAP_TOP + day * HEATMAP_CELL;
    ctx.fillText(WEEKDAY_LABELS[day], HEATMAP_LEFT - 16, y + HEATMAP_CELL * 0.62);
    for (let hour = 0; hour < 24; hour++) {
      const minutes = getHourMinutes(analytics, day, hour);
      ctx.fillStyle = cellColor(minutes);
      ctx.fillRect(HEATMAP_LEFT + hour * HEATMAP_CELL + 2, y + 2, HEATMAP_CELL - 4, HEATMAP_CELL - 4);
    }
  }

  ctx.strokeStyle = "#263143";
  ctx.lineWidth = 1;
  for (let hour = 0; hour <= 24; hour++) {
    const x = HEATMAP_LEFT + hour * HEATMAP_CELL;
    ctx.beginPath();
    ctx.moveTo(x, HEATMAP_TOP);
    ctx.lineTo(x, HEATMAP_TOP + HEATMAP_CELL * 7);
    ctx.stroke();
  }
  for (let day = 0; day <= 7; day++) {
    const y = HEATMAP_TOP + day * HEATMAP_CELL;
    ctx.beginPath();
    ctx.moveTo(HEATMAP_LEFT, y);
    ctx.lineTo(HEATMAP_LEFT + HEATMAP_WIDTH, y);
    ctx.stroke();
  }
}

function drawBars(ctx: any, analytics: TrackGroupAnalyticsResponse) {
  ctx.fillStyle = "#e8eef7";
  ctx.font = "700 22px Arial";
  ctx.textAlign = "left";
  ctx.fillText("Average By Hour", BAR_LEFT, BAR_TOP - 38);

  const values = averageHourlyMinutes(analytics);
  const max = Math.max(10, Math.ceil(Math.max(...values) / 10) * 10);
  const baseline = BAR_TOP + BAR_HEIGHT;
  const step = BAR_WIDTH / 24;
  const barW = Math.max(34, step * 0.72);

  ctx.strokeStyle = "#263143";
  ctx.fillStyle = "#8795aa";
  ctx.font = "13px Arial";
  ctx.textAlign = "right";
  for (let tick = 0; tick <= max; tick += Math.max(5, max / 5)) {
    const y = baseline - (tick / max) * BAR_HEIGHT;
    ctx.beginPath();
    ctx.moveTo(BAR_LEFT, y);
    ctx.lineTo(BAR_LEFT + BAR_WIDTH, y);
    ctx.stroke();
    ctx.fillText(String(Math.round(tick)), BAR_LEFT - 14, y + 4);
  }
  ctx.save();
  ctx.translate(BAR_LEFT - 72, BAR_TOP + BAR_HEIGHT / 2);
  ctx.rotate(-Math.PI / 2);
  ctx.textAlign = "center";
  ctx.fillText("minutes", 0, 0);
  ctx.restore();

  const gradient = ctx.createLinearGradient(0, baseline, 0, BAR_TOP);
  gradient.addColorStop(0, "#2d789f");
  gradient.addColorStop(0.65, "#de811d");
  gradient.addColorStop(1, "#f6d57b");
  for (let hour = 0; hour < 24; hour++) {
    const height = (values[hour] / max) * BAR_HEIGHT;
    const x = BAR_LEFT + hour * step + (step - barW) / 2;
    ctx.fillStyle = gradient;
    ctx.fillRect(x, baseline - height, barW, Math.max(2, height));

    ctx.fillStyle = "#8795aa";
    ctx.textAlign = "center";
    ctx.fillText(String(hour).padStart(2, "0"), BAR_LEFT + hour * step + step / 2, baseline + 22);
  }

  ctx.textAlign = "center";
  ctx.fillText("hour", BAR_LEFT + BAR_WIDTH / 2, baseline + 48);
}

function drawLegend(ctx: any) {
  const x = HEATMAP_LEFT + HEATMAP_WIDTH + 46;
  const y = HEATMAP_TOP;
  const w = 28;
  const h = HEATMAP_HEIGHT;
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
