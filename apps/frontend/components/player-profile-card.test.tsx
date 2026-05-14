import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import { PlayerProfileCard } from "./player-profile-card";

const baseProfile = {
	steamId: "76561198000000001",
	bmId: "123456",
	bmName: "Bean",
	personaName: "Beanster",
	avatarImage: "",
	firstSeenUnix: 1_700_000_000,
	hours: {
		vitality: 0,
		atlas: 0,
		vital: 4,
		modded: 8,
		official: 0,
		build: 0,
		bm: 12,
		rust: 50,
	},
	serversPlayed: 3,
	nameGaps: [],
	status: {
		isOnline: true,
		serverBmId: "111",
		serverName: "Vital Main",
		onlineUnix: 1_700_100_000,
		offlineUnix: null,
	},
};

describe("PlayerProfileCard", () => {
	it("renders display name, identity links, and stats", () => {
		render(<PlayerProfileCard profile={baseProfile} />);
		expect(screen.getByRole("heading", { name: "Bean" })).toBeInTheDocument();
		expect(screen.getByText(/BM #123456/)).toBeInTheDocument();
		expect(screen.getByText(/Steam 76561198000000001/)).toBeInTheDocument();
		expect(screen.getByText("12")).toBeInTheDocument();
		expect(screen.getByText("50")).toBeInTheDocument();
		expect(screen.getByText("3")).toBeInTheDocument();
	});

	it("renders online status pill when status.isOnline is true", () => {
		render(<PlayerProfileCard profile={baseProfile} />);
		expect(screen.getByText(/Online/)).toBeInTheDocument();
		expect(screen.getByText(/Vital Main/)).toBeInTheDocument();
	});

	it("shows category hours only for categories with hours > 0", () => {
		render(<PlayerProfileCard profile={baseProfile} />);
		expect(screen.getByText("Vital")).toBeInTheDocument();
		expect(screen.getByText("Modded")).toBeInTheDocument();
		expect(screen.queryByText("Atlas")).toBeNull();
		expect(screen.queryByText("Vitality")).toBeNull();
	});

	it("falls back to personaName then steamId when bmName is empty", () => {
		render(<PlayerProfileCard profile={{ ...baseProfile, bmName: "" }} />);
		expect(screen.getByRole("heading", { name: "Beanster" })).toBeInTheDocument();
	});
});
