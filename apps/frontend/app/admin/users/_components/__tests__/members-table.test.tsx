import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { MembersTable } from "../members-table";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

const toastSuccess = vi.fn();
const toastError = vi.fn();
const toastWarning = vi.fn();

vi.mock("sonner", () => ({
  toast: {
    success: (...args: unknown[]) => toastSuccess(...args),
    error: (...args: unknown[]) => toastError(...args),
    warning: (...args: unknown[]) => toastWarning(...args),
  },
}));

beforeEach(() => {
  toastSuccess.mockReset();
  toastError.mockReset();
  toastWarning.mockReset();
});

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

  it("does not render the bulk toolbar when nothing is selected", () => {
    render(
      <MembersTable
        initialQuery=""
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(2)}
        canSuspend={false}
      />,
    );

    expect(screen.queryByText(/selected/i)).not.toBeInTheDocument();
  });

  it("shows the bulk toolbar with the selected count and a Clear button", () => {
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
    fireEvent.click(rowChecks[2]);

    expect(screen.getByText("2 selected")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^clear$/i }));
    expect(screen.queryByText(/selected/i)).not.toBeInTheDocument();
  });

  it("copies selected Discord IDs to the clipboard and toasts", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

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
    fireEvent.click(rowChecks[2]);

    fireEvent.click(screen.getByRole("button", { name: /copy ids/i }));

    await waitFor(() => expect(writeText).toHaveBeenCalledTimes(1));
    expect(writeText).toHaveBeenCalledWith("1000000000000000\n1000000000000002");
    await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
    expect(toastSuccess.mock.calls[0][0]).toMatch(/2 discord ids/i);
  });
});
