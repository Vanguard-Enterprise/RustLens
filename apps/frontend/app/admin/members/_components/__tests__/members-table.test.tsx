import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import { MembersTable } from "../members-table";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

function fixturePage(items: number) {
  return {
    items: Array.from({ length: items }, (_, i) => ({
      user: {
        id:                `usr_${i}`,
        discordId:         `100000000000000${i}`,
        discordUsername:   `user${i}`,
        discordGlobalName: `User ${i}`,
        discordAvatar:     "",
        displayName:       `User ${i}`,
        avatarUrl:         "",
        roles:             ["user"],
        createdAt:         "2026-05-01T00:00:00Z",
        updatedAt:         "2026-05-01T00:00:00Z",
        lastLoginAt:       "2026-05-01T00:00:00Z",
      },
      discordId:           `100000000000000${i}`,
      discordUsername:     `user${i}`,
      discordGlobalName:   `User ${i}`,
      discordAvatar:       "",
      guildCount:          0,
      steamConnectionCount: 0,
      adminRoles:          [],
    })),
    total:    items,
    page:     1,
    pageSize: 20,
  };
}

describe("MembersTable", () => {
  it("renders one row per member with name and discord id", () => {
    render(
      <MembersTable
        initialQuery=""
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(2)}
      />,
    );

    expect(screen.getByText("User 0")).toBeInTheDocument();
    expect(screen.getByText("@user0")).toBeInTheDocument();
    expect(screen.getByText("1000000000000000")).toBeInTheDocument();
    expect(screen.getByText("User 1")).toBeInTheDocument();
  });

  it("renders an empty-state row when there are no members", () => {
    render(
      <MembersTable
        initialQuery=""
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(0)}
      />,
    );

    expect(screen.getByText("No members found.")).toBeInTheDocument();
  });
});
