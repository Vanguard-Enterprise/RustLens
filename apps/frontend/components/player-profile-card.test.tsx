import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";

import { PlayerProfileCard } from "./player-profile-card";

describe("PlayerProfileCard", () => {
	it("renders the public profile fields", () => {
		const { container } = render(
			<PlayerProfileCard
				profile={{
					steamId: "76561198000000001",
					bmId: "123456",
					bmName: "Bean",
					avatarImage: null,
					firstSeenAt: null,
					totalBmHours: 12.5,
					categoryHours: { Vital: 4, Modded: 8 },
					steamRustHours: 50,
					currentServer: { bmId: "111", name: "Vital Main" },
					lastServer: null,
					riskFactor: null,
				}}
			/>,
		);
		expect(container).toMatchSnapshot();
	});
});
