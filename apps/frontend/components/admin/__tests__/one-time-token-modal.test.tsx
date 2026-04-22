import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { OneTimeTokenModal } from "../one-time-token-modal";

describe("OneTimeTokenModal", () => {
  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    });
  });

  it("renders the URL when open", () => {
    render(
      <OneTimeTokenModal
        open
        url="https://example.com/reset?token=xyz"
        title="One-time link"
        onClose={() => {}}
      />,
    );
    const input = screen.getByLabelText("One-time URL") as HTMLInputElement;
    expect(input.value).toBe("https://example.com/reset?token=xyz");
  });

  it("Esc does NOT call onClose", () => {
    const onClose = vi.fn();
    render(
      <OneTimeTokenModal
        open
        url="https://example.com/x"
        title="t"
        onClose={onClose}
      />,
    );
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(onClose).not.toHaveBeenCalled();
  });

  it("explicit confirm button calls onClose", () => {
    const onClose = vi.fn();
    render(
      <OneTimeTokenModal
        open
        url="https://example.com/x"
        title="t"
        onClose={onClose}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /saved this link/i }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("copy button writes to clipboard", () => {
    render(
      <OneTimeTokenModal
        open
        url="https://example.com/x"
        title="t"
        onClose={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /copy link/i }));
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
      "https://example.com/x",
    );
  });

  it("returns null when url is null", () => {
    const { container } = render(
      <OneTimeTokenModal open url={null} title="t" onClose={() => {}} />,
    );
    expect(container.textContent).toBe("");
  });
});
