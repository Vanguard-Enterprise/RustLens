import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { MembersTable } from "../members-table";

const routerPush = vi.fn();
const routerRefresh = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: routerPush, refresh: routerRefresh }),
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

const bulkUser = vi.fn();
const suspendUser = vi.fn();
const unsuspendUser = vi.fn();
const deleteUser = vi.fn();
const setUserBan = vi.fn();

vi.mock("../../actions", () => ({
  setUserBanAction: (userId: string, categories: string[], reason: string) =>
    setUserBan(userId, categories, reason),
  suspendUserAction: (userId: string, reason: string) =>
    suspendUser(userId, reason),
  unsuspendUserAction: (userId: string, reason: string) =>
    unsuspendUser(userId, reason),
  deleteUserAction: (userId: string, reason: string, typed: string) =>
    deleteUser(userId, reason, typed),
  bulkUserAction: (
    action: string,
    userIds: string[],
    params: Record<string, unknown>,
    reason: string,
  ) => bulkUser(action, userIds, params, reason),
}));

beforeEach(() => {
  toastSuccess.mockReset();
  toastError.mockReset();
  toastWarning.mockReset();
  bulkUser.mockReset();
  suspendUser.mockReset();
  unsuspendUser.mockReset();
  deleteUser.mockReset();
  setUserBan.mockReset();
  routerPush.mockReset();
  routerRefresh.mockReset();
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
      hasUserAppInstalled: false,
      banCategories:       [],
    })),
    total:    items,
    page:     1,
    pageSize: 20,
  };
}

describe("MembersTable", () => {
  it("renders one row per member with name and discord handle", () => {
    render(
      <MembersTable
        initialQuery=""
        initialStatus="active"
        roles={[]}
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(2)}
        canSuspend={false}
      />,
    );

    expect(screen.getByText("User 0")).toBeInTheDocument();
    expect(screen.getByText("@user0")).toBeInTheDocument();
    expect(screen.getByText("User 1")).toBeInTheDocument();
    expect(screen.queryByText("1000000000000000")).not.toBeInTheDocument();
  });

  it("renders an empty-state row when there are no members", () => {
    render(
      <MembersTable
        initialQuery=""
        initialStatus="active"
        roles={[]}
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
        initialStatus="active"
        roles={[]}
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
        initialStatus="active"
        roles={[]}
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
        initialStatus="active"
        roles={[]}
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
        initialStatus="active"
        roles={[]}
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
        initialStatus="active"
        roles={[]}
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

  it("bulk suspend opens a dialog titled with the count and dispatches the action", async () => {
    bulkUser.mockResolvedValue({
      ok: true,
      result: {
        action: "suspend",
        succeeded: 2,
        failed: 0,
        results: [
          { userId: "usr_0", ok: true },
          { userId: "usr_2", ok: true },
        ],
      },
    });

    render(
      <MembersTable
        initialQuery=""
        initialStatus="active"
        roles={[]}
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(3)}
        canSuspend
      />,
    );

    const rowChecks = screen.getAllByRole("checkbox", { name: /select row/i });
    fireEvent.click(rowChecks[0]);
    fireEvent.click(rowChecks[2]);

    fireEvent.click(screen.getByRole("button", { name: /^suspend$/i }));

    expect(
      await screen.findByRole("heading", { name: /suspend 2 users/i }),
    ).toBeInTheDocument();

    const reasonInput = screen.getByLabelText(/^reason$/i);
    fireEvent.change(reasonInput, { target: { value: "spamming" } });

    fireEvent.click(
      screen.getByRole("button", { name: /^suspend$/i, hidden: false }),
    );

    await waitFor(() => expect(bulkUser).toHaveBeenCalledTimes(1));
    expect(bulkUser).toHaveBeenCalledWith(
      "suspend",
      ["usr_0", "usr_2"],
      {},
      "spamming",
    );
    await waitFor(() => expect(toastSuccess).toHaveBeenCalled());
    expect(toastSuccess.mock.calls[0][0]).toMatch(/2 users suspended/i);
  });

  it("bulk unsuspend dispatches the unsuspend sub-action", async () => {
    bulkUser.mockResolvedValue({
      ok: true,
      result: {
        action: "unsuspend",
        succeeded: 1,
        failed: 0,
        results: [{ userId: "usr_1", ok: true }],
      },
    });

    render(
      <MembersTable
        initialQuery=""
        initialStatus="active"
        roles={[]}
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(2)}
        canSuspend
      />,
    );

    const rowChecks = screen.getAllByRole("checkbox", { name: /select row/i });
    fireEvent.click(rowChecks[1]);

    fireEvent.click(screen.getByRole("button", { name: /^unsuspend$/i }));
    expect(
      await screen.findByRole("heading", { name: /unsuspend 1 user/i }),
    ).toBeInTheDocument();

    fireEvent.click(
      screen.getAllByRole("button", { name: /^unsuspend$/i }).at(-1)!,
    );

    await waitFor(() => expect(bulkUser).toHaveBeenCalledTimes(1));
    expect(bulkUser).toHaveBeenCalledWith("unsuspend", ["usr_1"], {}, "");
  });

  it("partial bulk failures produce a warning toast", async () => {
    bulkUser.mockResolvedValue({
      ok: true,
      result: {
        action: "suspend",
        succeeded: 1,
        failed: 1,
        results: [
          { userId: "usr_0", ok: true },
          { userId: "usr_1", ok: false, error: "already_suspended" },
        ],
      },
    });

    render(
      <MembersTable
        initialQuery=""
        initialStatus="active"
        roles={[]}
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(2)}
        canSuspend
      />,
    );

    const rowChecks = screen.getAllByRole("checkbox", { name: /select row/i });
    fireEvent.click(rowChecks[0]);
    fireEvent.click(rowChecks[1]);

    fireEvent.click(screen.getByRole("button", { name: /^suspend$/i }));
    fireEvent.change(screen.getByLabelText(/^reason$/i), {
      target: { value: "spamming" },
    });
    fireEvent.click(
      screen.getAllByRole("button", { name: /^suspend$/i }).at(-1)!,
    );

    await waitFor(() => expect(toastWarning).toHaveBeenCalled());
    expect(toastWarning.mock.calls[0][0]).toMatch(/1 of 2/i);
  });

  it("does not render bulk suspend buttons without canSuspend", () => {
    render(
      <MembersTable
        initialQuery=""
        initialStatus="active"
        roles={[]}
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(2)}
        canSuspend={false}
      />,
    );

    fireEvent.click(
      screen.getAllByRole("checkbox", { name: /select row/i })[0],
    );

    expect(
      screen.queryByRole("button", { name: /^suspend$/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^unsuspend$/i }),
    ).not.toBeInTheDocument();
  });

  it("opens user restrictions and saves selected categories", async () => {
    setUserBan.mockResolvedValue({ ok: true, result: { userId: "usr_0" } });
    const data = fixturePage(1);
    data.items[0].banCategories = ["lookup"];

    render(
      <MembersTable
        initialQuery=""
        initialStatus="active"
        roles={[]}
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={data}
        canSuspend
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /manage user restrictions/i }));

    expect(
      await screen.findByRole("heading", { name: /restrict user 0/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Player And Account Lookup").closest("label")?.querySelector("[role='checkbox']"),
    ).toHaveAttribute("data-state", "checked");

    fireEvent.click(screen.getByText("Activity, Presence, And Checks"));
    fireEvent.change(screen.getByLabelText(/^reason$/i), {
      target: { value: "command abuse" },
    });
    fireEvent.click(screen.getByRole("button", { name: /save restrictions/i }));

    await waitFor(() => expect(setUserBan).toHaveBeenCalledTimes(1));
    expect(setUserBan).toHaveBeenCalledWith(
      "usr_0",
      ["lookup", "activity"],
      "command abuse",
    );
    await waitFor(() => expect(routerRefresh).toHaveBeenCalledTimes(1));
  });

  it("clicking a row checkbox does not trigger row navigation", () => {
    render(
      <MembersTable
        initialQuery=""
        initialStatus="active"
        roles={[]}
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(2)}
        canSuspend={false}
      />,
    );

    fireEvent.click(
      screen.getAllByRole("checkbox", { name: /select row/i })[0],
    );

    expect(routerPush).not.toHaveBeenCalled();
  });

  it("calls router.refresh after a successful bulk suspend", async () => {
    bulkUser.mockResolvedValue({
      ok: true,
      result: {
        action: "suspend",
        succeeded: 1,
        failed: 0,
        results: [{ userId: "usr_0", ok: true }],
      },
    });

    render(
      <MembersTable
        initialQuery=""
        initialStatus="active"
        roles={[]}
        pageSizeOptions={[10, 20, 30, 40, 50]}
        data={fixturePage(2)}
        canSuspend
      />,
    );

    fireEvent.click(
      screen.getAllByRole("checkbox", { name: /select row/i })[0],
    );
    fireEvent.click(screen.getByRole("button", { name: /^suspend$/i }));
    fireEvent.change(screen.getByLabelText(/^reason$/i), {
      target: { value: "spamming" },
    });
    fireEvent.click(
      screen.getAllByRole("button", { name: /^suspend$/i }).at(-1)!,
    );

    await waitFor(() => expect(routerRefresh).toHaveBeenCalledTimes(1));
  });
});
