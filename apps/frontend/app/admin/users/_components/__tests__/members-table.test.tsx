import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

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
        canSuspend={false}
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
        canSuspend={false}
      />,
    );

    expect(screen.getByText("No members found.")).toBeInTheDocument();
  });

  it("header checkbox selects all rows on the current page", () => {
    render(
      <MembersTable
        initialQuery=""
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(3)}
        canSuspend={false}
      />,
    );

    const selectAll = screen.getByRole("checkbox", { name: /select all/i });
    expect(selectAll).toHaveAttribute("data-state", "unchecked");

    fireEvent.click(selectAll);
    expect(selectAll).toHaveAttribute("data-state", "checked");

    const rowChecks = screen
      .getAllByRole("checkbox", { name: /select row/i });
    expect(rowChecks).toHaveLength(3);
    for (const c of rowChecks) {
      expect(c).toHaveAttribute("data-state", "checked");
    }
  });

  it("header checkbox is indeterminate when a subset is selected", () => {
    render(
      <MembersTable
        initialQuery=""
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(3)}
        canSuspend={false}
      />,
    );

    const rowChecks = screen.getAllByRole("checkbox", { name: /select row/i });
    fireEvent.click(rowChecks[0]);

    const selectAll = screen.getByRole("checkbox", { name: /select all/i });
    expect(selectAll).toHaveAttribute("data-state", "indeterminate");
  });
});
