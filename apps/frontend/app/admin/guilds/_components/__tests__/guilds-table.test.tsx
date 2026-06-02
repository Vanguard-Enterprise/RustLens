import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { GuildsTable } from "../guilds-table";

const routerPush = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: routerPush }),
  useSearchParams: () => new URLSearchParams(),
}));

beforeEach(() => {
  routerPush.mockReset();
});

function fixturePage() {
  return {
    items: [
      {
        guildId: "111111111111111111",
        name: "Alpha",
        iconHash: null,
        ownerDiscordId: "222222222222222222",
        ownerUsername: "owner",
        ownerGlobalName: null,
        ownerAvatarHash: null,
        approximateMemberCount: 42,
        installedAt: "2026-02-01T00:00:00Z",
        isInstalled: true,
        leftAt: null,
        isConfigured: true,
        trackedServerCount: 2,
      },
    ],
    total: 1,
    page: 1,
    pageSize: 20,
  };
}

describe("GuildsTable", () => {
  it("renders joined date, uses users-style footer, and sorts members", () => {
    render(
      <GuildsTable
        initialQuery=""
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage()}
      />,
    );

    expect(screen.getByTitle("2026-02-01T00:00:00Z")).toBeInTheDocument();
    expect(screen.getByLabelText("Rows per page")).toBeInTheDocument();
    expect(screen.getByText("1 guild")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /sort by members/i }));
    expect(routerPush).toHaveBeenCalledWith("?sort=-members");
  });
});
