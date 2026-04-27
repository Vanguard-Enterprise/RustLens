import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { AdminActionButton } from "../admin-action-button";

describe("AdminActionButton", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("opens reason dialog for reason-required action", async () => {
    // users.roles.grant is reason-required and not dangerous, so it uses the
    // plain reason dialog.
    render(
      <AdminActionButton
        action="users.roles.grant"
        params={{ userId: "u1", roleKey: "moderator" }}
        csrfToken="t"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /grant role/i }));
    await waitFor(() =>
      expect(screen.getByPlaceholderText(/reason/i)).toBeInTheDocument(),
    );
  });

  it("POSTs to the dispatcher on confirm", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    render(
      <AdminActionButton
        action="users.roles.grant"
        params={{ userId: "u1", roleKey: "moderator" }}
        csrfToken="t"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /grant role/i }));
    // findAllByPlaceholderText used because Radix UI keeps aria-hidden portals in DOM (multiple matches)
    const [reasonInput] = await screen.findAllByPlaceholderText(/reason/i);
    fireEvent.change(reasonInput, { target: { value: "promotion" } });
    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", { name: /confirm/i, hidden: true }),
      );
    });
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
      const [url, init] = fetchMock.mock.calls[0];
      expect(String(url)).toContain(
        "/api/app/admin/actions/users.roles.grant",
      );
      const body = JSON.parse(String((init as RequestInit).body));
      expect(body.reason).toBe("promotion");
      expect(body.params.userId).toBe("u1");
    });
  });
});
