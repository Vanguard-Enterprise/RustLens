import { createCanvas } from "@napi-rs/canvas";
import type { ServerAnalyticsResponse } from "./backend-client.js";

const WIDTH = 1600;
const HEIGHT = 900;
const PADDING = 64;
const CHART_LEFT = 112;
const LINE_TOP = 140;
const LINE_WIDTH = 1376;
const LINE_HEIGHT = 380;
const BAR_TOP = 625;
const BAR_WIDTH = 1376;
const BAR_HEIGHT = 210;
const FONT_FAMILY = "DejaVu Sans, Arial, sans-serif";
const WEEKDAY_LABELS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const MAX_COLOR = "#f6d57b";
const AVG_COLOR = "#2d789f";
const MIN_COLOR = "#8795aa";
const TREND_COLOR = "#d31239";
const PEAK_TREND_COLOR = "#22c55e";
const PEAK_TREND_MIN_PEAK_RATIO = 0.5;

function maxPop(analytics: ServerAnalyticsResponse) {
  const lineMax = Math.max(0, ...analytics.points.flatMap((point) => [point.min, point.value, point.max]));
  const barMax = Math.max(0, ...analytics.weekdays.flatMap((day) => [day.min, day.value, day.max]));
  return Math.max(10, Math.ceil(Math.max(lineMax, barMax) / 25) * 25);
}

function formatDateLabel(timestampUnix: number) {
  return new Intl.DateTimeFormat("en-GB", { day: "2-digit", month: "short", timeZone: "UTC" }).format(new Date(timestampUnix * 1000));
}

export async function renderServerAnalyticsChart(analytics: ServerAnalyticsResponse) {
  const canvas = createCanvas(WIDTH, HEIGHT);
  const ctx = canvas.getContext("2d");
  const background = ctx.createLinearGradient(0, 0, WIDTH, HEIGHT);
  background.addColorStop(0, "#090d15");
  background.addColorStop(1, "#121827");
  ctx.fillStyle = background;
  ctx.fillRect(0, 0, WIDTH, HEIGHT);

  ctx.fillStyle = "#9aa8bd";
  ctx.font = `18px ${FONT_FAMILY}`;
  ctx.textAlign = "left";
  ctx.fillText(`${analytics.serverName} | Trend: ${analytics.timeframe} | Weekday distribution: ${analytics.cacheWindow} average`, PADDING, 56);

  const max = maxPop(analytics);
  drawLineChart(ctx, analytics, max);
  drawWeekdayBars(ctx, analytics, max);
  drawLegend(ctx, analytics);

  return canvas.encode("png");
}

function drawLineChart(ctx: any, analytics: ServerAnalyticsResponse, max: number) {
  ctx.fillStyle = "#e8eef7";
  ctx.font = `700 22px ${FONT_FAMILY}`;
  ctx.textAlign = "left";
  ctx.fillText("Player Count History", CHART_LEFT, LINE_TOP - 48);

  const baseline = LINE_TOP + LINE_HEIGHT;
  drawGrid(ctx, CHART_LEFT, LINE_TOP, LINE_WIDTH, LINE_HEIGHT, max, "pop");

  const points = [...analytics.points].sort((a, b) => a.timestampUnix - b.timestampUnix);
  if (points.length === 0) {
    ctx.fillStyle = "#8795aa";
    ctx.font = `18px ${FONT_FAMILY}`;
    ctx.textAlign = "center";
    ctx.fillText("No player-count history returned for this range", CHART_LEFT + LINE_WIDTH / 2, LINE_TOP + LINE_HEIGHT / 2);
    return;
  }

  const minTime = points[0].timestampUnix;
  const maxTime = points[points.length - 1].timestampUnix;
  const timeSpan = Math.max(1, maxTime - minTime);
  const xFor = (timestampUnix: number) => CHART_LEFT + ((timestampUnix - minTime) / timeSpan) * LINE_WIDTH;
  const yFor = (value: number) => baseline - (Math.min(max, Math.max(0, value)) / max) * LINE_HEIGHT;

  drawSeries(ctx, points.map((point) => [xFor(point.timestampUnix), yFor(point.max)]), MAX_COLOR);
  drawSeries(ctx, points.map((point) => [xFor(point.timestampUnix), yFor(point.value)]), AVG_COLOR);
  drawSeries(ctx, points.map((point) => [xFor(point.timestampUnix), yFor(point.min)]), MIN_COLOR);
  drawTrend(ctx, averageTrendLine(points), xFor, yFor, TREND_COLOR);
  drawTrend(ctx, peakTrendLine(points), xFor, yFor, PEAK_TREND_COLOR);

  ctx.fillStyle = "#8795aa";
  ctx.font = `13px ${FONT_FAMILY}`;
  ctx.textAlign = "center";
  const labels = Math.min(6, points.length);
  for (let i = 0; i < labels; i++) {
    const index = labels === 1 ? 0 : Math.round((i / (labels - 1)) * (points.length - 1));
    const point = points[index];
    ctx.fillText(formatDateLabel(point.timestampUnix), xFor(point.timestampUnix), baseline + 24);
  }
  ctx.fillText("date", CHART_LEFT + LINE_WIDTH / 2, baseline + 52);
}

function drawSeries(ctx: any, coords: number[][], color: string) {
  if (coords.length === 0) {
    return;
  }
  ctx.strokeStyle = color;
  ctx.lineWidth = 4;
  ctx.lineJoin = "round";
  ctx.lineCap = "round";
  ctx.beginPath();
  ctx.moveTo(coords[0][0], coords[0][1]);
  for (const [x, y] of coords.slice(1)) {
    ctx.lineTo(x, y);
  }
  ctx.stroke();
}

function acceptedPeakTrendPoints(points: ServerAnalyticsResponse["points"]) {
  const peak = Math.max(0, ...points.map((point) => point.max));
  const minimumHighPop = peak * PEAK_TREND_MIN_PEAK_RATIO;
  return points.filter((point) => point.max >= minimumHighPop);
}

function trendLine(
  fitPoints: ServerAnalyticsResponse["points"],
  valueFor: (point: ServerAnalyticsResponse["points"][number]) => number,
  rangePoints = fitPoints,
) {
  if (fitPoints.length < 2 || rangePoints.length < 2) {
    return null;
  }

  const firstTime = fitPoints[0].timestampUnix;
  const values = fitPoints.map((point) => ({
    x: (point.timestampUnix - firstTime) / 86400,
    y: valueFor(point),
  }));
  const count = values.length;
  const sumX = values.reduce((sum, point) => sum + point.x, 0);
  const sumY = values.reduce((sum, point) => sum + point.y, 0);
  const sumXY = values.reduce((sum, point) => sum + point.x * point.y, 0);
  const sumXX = values.reduce((sum, point) => sum + point.x * point.x, 0);
  const denominator = count * sumXX - sumX * sumX;
  if (denominator === 0) {
    return null;
  }

  const slope = (count * sumXY - sumX * sumY) / denominator;
  const intercept = (sumY - slope * sumX) / count;
  const start = rangePoints[0];
  const end = rangePoints[rangePoints.length - 1];
  return {
    startTimestampUnix: start.timestampUnix,
    endTimestampUnix: end.timestampUnix,
    startValue: intercept + slope * ((start.timestampUnix - firstTime) / 86400),
    endValue: intercept + slope * ((end.timestampUnix - firstTime) / 86400),
  };
}

function averageTrendLine(points: ServerAnalyticsResponse["points"]) {
  return trendLine(points, (point) => point.value);
}

function peakTrendLine(points: ServerAnalyticsResponse["points"]) {
  return trendLine(acceptedPeakTrendPoints(points), (point) => point.max, points);
}

function drawTrend(
  ctx: any,
  trend: ReturnType<typeof trendLine>,
  xFor: (timestampUnix: number) => number,
  yFor: (value: number) => number,
  color: string,
) {
  if (!trend) {
    return;
  }

  ctx.strokeStyle = color;
  ctx.lineWidth = 4;
  ctx.setLineDash([18, 12]);
  ctx.beginPath();
  ctx.moveTo(xFor(trend.startTimestampUnix), yFor(trend.startValue));
  ctx.lineTo(xFor(trend.endTimestampUnix), yFor(trend.endValue));
  ctx.stroke();
  ctx.setLineDash([]);
}

function sortedAnalyticsPoints(analytics: ServerAnalyticsResponse) {
  return [...analytics.points].sort((a, b) => a.timestampUnix - b.timestampUnix);
}

function formatTrendDelta(trend: ReturnType<typeof trendLine>) {
  if (!trend) {
    return "";
  }
  const start = Math.max(0, trend.startValue);
  const end = Math.max(0, trend.endValue);
  const percent = ((end - start) / Math.max(1, start)) * 100;
  const rounded = Math.round(percent);
  const sign = rounded >= 0 ? "+" : "";
  return ` ${sign}${rounded}%`;
}

function drawWeekdayBars(ctx: any, analytics: ServerAnalyticsResponse, max: number) {
  ctx.fillStyle = "#e8eef7";
  ctx.font = `700 22px ${FONT_FAMILY}`;
  ctx.textAlign = "left";
  ctx.fillText("Average By Weekday", CHART_LEFT, BAR_TOP - 38);

  const baseline = BAR_TOP + BAR_HEIGHT;
  drawGrid(ctx, CHART_LEFT, BAR_TOP, BAR_WIDTH, BAR_HEIGHT, max, "pop");

  const step = BAR_WIDTH / 7;
  const barW = Math.max(26, step * 0.18);
  const values = analytics.weekdays;

  for (let day = 0; day < 7; day++) {
    const item = values[day] ?? { min: 0, value: 0, max: 0 };
    const center = CHART_LEFT + day * step + step / 2;
    drawBar(ctx, center - barW * 1.2, baseline, barW, BAR_HEIGHT, max, item.max, MAX_COLOR);
    drawBar(ctx, center, baseline, barW, BAR_HEIGHT, max, item.value, AVG_COLOR);
    drawBar(ctx, center + barW * 1.2, baseline, barW, BAR_HEIGHT, max, item.min, MIN_COLOR);

    ctx.fillStyle = "#8795aa";
    ctx.font = `13px ${FONT_FAMILY}`;
    ctx.textAlign = "center";
    ctx.fillText(WEEKDAY_LABELS[day], center, baseline + 24);
  }
  ctx.fillText("weekday", CHART_LEFT + BAR_WIDTH / 2, baseline + 52);
}

function drawBar(ctx: any, center: number, baseline: number, width: number, height: number, max: number, value: number, color: string) {
  const barHeight = (value / max) * height;
  ctx.fillStyle = color;
  ctx.fillRect(center - width / 2, baseline - barHeight, width, Math.max(2, barHeight));
}

function drawGrid(ctx: any, left: number, top: number, width: number, height: number, max: number, label: string) {
  const baseline = top + height;
  ctx.strokeStyle = "#263143";
  ctx.fillStyle = "#8795aa";
  ctx.font = `13px ${FONT_FAMILY}`;
  ctx.textAlign = "right";
  for (let tick = 0; tick <= max; tick += Math.max(5, max / 5)) {
    const rounded = Math.round(tick);
    const y = baseline - (rounded / max) * height;
    ctx.beginPath();
    ctx.moveTo(left, y);
    ctx.lineTo(left + width, y);
    ctx.stroke();
    ctx.fillText(String(rounded), left - 14, y + 4);
  }
  ctx.save();
  ctx.translate(left - 72, top + height / 2);
  ctx.rotate(-Math.PI / 2);
  ctx.textAlign = "center";
  ctx.fillText(label, 0, 0);
  ctx.restore();
}

function drawLegend(ctx: any, analytics: ServerAnalyticsResponse) {
  const points = sortedAnalyticsPoints(analytics);
  const averageTrend = averageTrendLine(points);
  const peakTrend = peakTrendLine(points);
  const items = [
    ["Max pop", MAX_COLOR],
    ["Avg pop", AVG_COLOR],
    ["Min pop", MIN_COLOR],
    [`Trend${formatTrendDelta(averageTrend)}`, TREND_COLOR],
    [`Peak trend${formatTrendDelta(peakTrend)}`, PEAK_TREND_COLOR],
  ];
  let x = WIDTH - 760;
  const y = 104;
  for (const [label, color] of items) {
    ctx.fillStyle = color;
    ctx.fillRect(x, y - 12, 28, 8);
    ctx.fillStyle = "#bfcbda";
    ctx.font = `14px ${FONT_FAMILY}`;
    ctx.textAlign = "left";
    ctx.fillText(label, x + 38, y - 4);
    x += 150;
  }
}
