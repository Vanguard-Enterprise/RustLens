import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import { AuditDetailSheet } from "../audit-detail-sheet";
import type { AuditEventItem } from "@/lib/rustlens-api";

const sample: AuditEventItem = {
	id: "evt_1",
	actorUserId: "u_a",
	targetUserId: "u_t",
	eventType: "users.suspend",
	payload: { foo: "bar" },
	createdAt: "2026-05-06T10:00:00Z",
	actorDisplayName: "Alice",
	actorAvatarUrl: null,
	targetDisplayName: "Bob",
	targetAvatarUrl: null,
	result: "success",
	reason: "abuse",
};

describe("AuditDetailSheet", () => {
	it("renders payload as JSON", () => {
		render(<AuditDetailSheet event={sample} status="ok" onClose={() => {}} />);
		expect(screen.getByText(/"foo": "bar"/)).toBeInTheDocument();
	});

	it("renders not-found state", () => {
		const onClose = vi.fn();
		render(<AuditDetailSheet event={null} status="not-found" onClose={onClose} />);
		expect(screen.getByText(/event not found/i)).toBeInTheDocument();
	});

	it("copy link writes window.location.href to clipboard", async () => {
		const writeText = vi.fn();
		Object.assign(navigator, { clipboard: { writeText } });
		render(<AuditDetailSheet event={sample} status="ok" onClose={() => {}} />);
		fireEvent.click(screen.getByRole("button", { name: /copy link/i }));
		expect(writeText).toHaveBeenCalledWith(window.location.href);
	});
});
