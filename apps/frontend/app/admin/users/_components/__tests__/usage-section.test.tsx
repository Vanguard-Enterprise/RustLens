import { describe, expect, test, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { UsageSection } from "../usage-section";

vi.mock("../../actions", () => ({
	getUserCommandUsageTargetsAction: vi.fn().mockResolvedValue({ targets: [] }),
	getUserCommandUsageChildrenAction: vi.fn().mockResolvedValue({
		items: [{
			id: 2,
			appUserId: "usr_test",
			discordId: "111",
			guildId: "g1",
			channelId: "c1",
			interactionType: "modal",
			command: "bind_c4_modal",
			subcommand: null,
			target: null,
			options: { bind_c4_bind: "Z", bind_c4_amount: "5" },
			outcome: "success",
			errorCode: null,
			latencyMs: 30,
			createdAt: new Date().toISOString(),
			targets: [],
		}],
	}),
}));

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
		expect(screen.getByText(/No bot usage matches the filters/i)).toBeInTheDocument();
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
				targets: [],
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
				targets: [],
			}],
		});
		render(<UsageSection userId={userId} data={data} />);

		// New layout: row is a TableRow (no button role), find the command code cell and click the row
		const commandCell = screen.getByText("/player");
		fireEvent.click(commandCell.closest("tr")!);
		expect(screen.getByText(/"group_name"/)).toBeInTheDocument();
		expect(screen.getByText(/"raid"/)).toBeInTheDocument();
	});

	test("expanded parent loads and renders linked follow-ups", async () => {
		const data = makeData({
			total: 1,
			summary: { totalCount: 1, errorCount: 0, errorRate: 0, topCommands: [] },
			items: [{
				id: 1,
				appUserId: userId,
				discordId: "111",
				guildId: "g1",
				channelId: "c1",
				interactionType: "command",
				command: "bind",
				subcommand: "c4",
				target: null,
				options: {},
				outcome: "success",
				errorCode: null,
				latencyMs: 50,
				createdAt: new Date().toISOString(),
				targets: [],
				childCount: 1,
			}],
		});
		render(<UsageSection userId={userId} data={data} />);

		// The +N badge surfaces the follow-up count up at the top-level row.
		expect(screen.getByText("+1")).toBeInTheDocument();

		// Expand the row and wait for the children fetch to resolve.
		const cell = screen.getByText("/bind c4");
		fireEvent.click(cell.closest("tr")!);
		expect(await screen.findByText(/Follow-ups \(1\)/)).toBeInTheDocument();
		expect(await screen.findByText(/bind_c4_bind/)).toBeInTheDocument();
	});

	test("Where column renders guild avatar + name when the row has guild context", () => {
		const data = makeData({
			total: 1,
			summary: { totalCount: 1, errorCount: 0, errorRate: 0, topCommands: [] },
			items: [{
				id: 1,
				appUserId: userId,
				discordId: "111",
				guildId: "g1",
				channelId: "c1",
				interactionType: "command",
				command: "player",
				subcommand: null,
				target: null,
				options: {},
				outcome: "success",
				errorCode: null,
				latencyMs: 12,
				createdAt: new Date().toISOString(),
				targets: [],
				channelKind: "guild",
				guildName: "Raid Squad",
				guildIcon: null,
			}],
		});
		render(<UsageSection userId={userId} data={data} />);
		// Guild name appears in the new Where column.
		expect(screen.getByText("Raid Squad")).toBeInTheDocument();
	});

	test("Where column shows channel name and GC badge for group DMs", () => {
		const data = makeData({
			total: 1,
			summary: { totalCount: 1, errorCount: 0, errorRate: 0, topCommands: [] },
			items: [{
				id: 1,
				appUserId: userId,
				discordId: "111",
				guildId: null,
				channelId: "c1",
				interactionType: "command",
				command: "ping",
				subcommand: null,
				target: null,
				options: {},
				outcome: "success",
				errorCode: null,
				latencyMs: 8,
				createdAt: new Date().toISOString(),
				targets: [],
				channelKind: "group_dm",
				channelName: "Wipe Crew",
			}],
		});
		render(<UsageSection userId={userId} data={data} />);
		expect(screen.getByText("Wipe Crew")).toBeInTheDocument();
		expect(screen.getByText("GC")).toBeInTheDocument();
	});

	test("Where column shows DM badge for 1:1 DM rows", () => {
		const data = makeData({
			total: 1,
			summary: { totalCount: 1, errorCount: 0, errorRate: 0, topCommands: [] },
			items: [{
				id: 1,
				appUserId: userId,
				discordId: "111",
				guildId: null,
				channelId: "c1",
				interactionType: "command",
				command: "ping",
				subcommand: null,
				target: null,
				options: {},
				outcome: "success",
				errorCode: null,
				latencyMs: 8,
				createdAt: new Date().toISOString(),
				targets: [],
				channelKind: "dm",
			}],
		});
		render(<UsageSection userId={userId} data={data} />);
		expect(screen.getByText("DM")).toBeInTheDocument();
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
				targets: [],
			}],
		});
		render(<UsageSection userId={userId} data={data} />);
		expect(screen.getAllByText(/error/i)[0]).toBeInTheDocument();
		// errorCode visible after expansion — click the row via the command code cell
		const commandCell = screen.getByText("/track create");
		fireEvent.click(commandCell.closest("tr")!);
		expect(screen.getByText(/boom/)).toBeInTheDocument();
	});
});
