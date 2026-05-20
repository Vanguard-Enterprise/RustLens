import { describe, expect, test } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { UsageSection } from "../usage-section";

const userId = "usr_test";

function makeData(overrides: Partial<Parameters<typeof UsageSection>[0]["data"]> = {}) {
	return {
		items: [],
		total: 0,
		page: 1,
		pageSize: 25,
		summary: {
			totalCount:  0,
			errorCount:  0,
			errorRate:   0,
			topCommands: [],
		},
		...overrides,
	};
}

describe("UsageSection", () => {
	test("renders empty state when no items", () => {
		render(<UsageSection userId={userId} data={makeData()} />);
		expect(screen.getByText(/No bot usage in the last 90 days/i)).toBeInTheDocument();
	});

	test("renders summary strip", () => {
		const data = makeData({
			total: 42,
			summary: {
				totalCount: 42,
				errorCount: 2,
				errorRate:  2 / 42,
				topCommands: [
					{ command: "player", count: 30 },
					{ command: "track",  count: 12 },
				],
			},
			items: [{
				id: 1,
				appUserId: userId,
				discordId: "111",
				guildId: "g1",
				channelId: "c1",
				interactionType: "command",
				command: "player",
				subcommand: null,
				target: "76561198000000001",
				options: { identifier: "76561198000000001" },
				outcome: "success",
				errorCode: null,
				latencyMs: 142,
				createdAt: new Date().toISOString(),
			}],
		});
		render(<UsageSection userId={userId} data={data} />);

		expect(screen.getAllByText(/42/)[0]).toBeInTheDocument();         // total
		expect(screen.getAllByText(/player/)[0]).toBeInTheDocument();    // top command label
		expect(screen.getByText(/track/)).toBeInTheDocument();
	});

	test("row click expands raw options JSON", () => {
		const data = makeData({
			total: 1,
			summary: { totalCount: 1, errorCount: 0, errorRate: 0, topCommands: [{ command: "player", count: 1 }] },
			items: [{
				id: 1,
				appUserId: userId,
				discordId: "111",
				guildId: "g1",
				channelId: "c1",
				interactionType: "command",
				command: "player",
				subcommand: null,
				target: "raid",
				options: { group_name: "raid" },
				outcome: "success",
				errorCode: null,
				latencyMs: 100,
				createdAt: new Date().toISOString(),
			}],
		});
		render(<UsageSection userId={userId} data={data} />);

		const row = screen.getByRole("button", { name: /player/i });
		fireEvent.click(row);
		expect(screen.getByText(/"group_name"/)).toBeInTheDocument();
		expect(screen.getByText(/"raid"/)).toBeInTheDocument();
	});

	test("error outcome renders destructive badge", () => {
		const data = makeData({
			total: 1,
			summary: { totalCount: 1, errorCount: 1, errorRate: 1, topCommands: [] },
			items: [{
				id: 1,
				appUserId: userId,
				discordId: "111",
				guildId: null,
				channelId: null,
				interactionType: "command",
				command: "track",
				subcommand: "create",
				target: null,
				options: {},
				outcome: "error",
				errorCode: "boom",
				latencyMs: 5,
				createdAt: new Date().toISOString(),
			}],
		});
		render(<UsageSection userId={userId} data={data} />);
		expect(screen.getAllByText(/error/i)[0]).toBeInTheDocument();
		// errorCode visible after expansion
		fireEvent.click(screen.getByRole("button", { name: /track/i }));
		expect(screen.getByText(/boom/)).toBeInTheDocument();
	});
});
