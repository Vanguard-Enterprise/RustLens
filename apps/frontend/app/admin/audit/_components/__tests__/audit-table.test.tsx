import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import { AuditTable } from "../audit-table";
import type { AuditEventItem } from "@/lib/rustlens-api";

const sample: AuditEventItem = {
	id: "evt_1",
	actorUserId: "u_a",
	targetUserId: "u_t",
	eventType: "users.suspend",
	payload: {},
	createdAt: "2026-05-06T10:00:00Z",
	actorDisplayName: "Alice",
	actorAvatarUrl: null,
	targetDisplayName: "Bob",
	targetAvatarUrl: null,
	result: "success",
	reason: "abuse",
};

describe("AuditTable", () => {
	it("row click invokes onSelect with id", () => {
		const onSelect = vi.fn();
		render(
			<AuditTable items={[sample]} selectedId={null} onSelect={onSelect} hasFilters={false} onClearFilters={() => {}} />,
		);
		fireEvent.click(screen.getByText("Users · Suspend"));
		expect(onSelect).toHaveBeenCalledWith("evt_1");
	});

	it("renders dash for null result", () => {
		const item = { ...sample, result: null as null };
		render(<AuditTable items={[item]} selectedId={null} onSelect={() => {}} hasFilters={false} onClearFilters={() => {}} />);
		expect(screen.getByText("—")).toBeInTheDocument();
	});

	it("empty state varies with hasFilters", () => {
		const { rerender } = render(
			<AuditTable items={[]} selectedId={null} onSelect={() => {}} hasFilters={false} onClearFilters={() => {}} />,
		);
		expect(screen.getByText(/no audit events yet/i)).toBeInTheDocument();
		rerender(<AuditTable items={[]} selectedId={null} onSelect={() => {}} hasFilters={true} onClearFilters={() => {}} />);
		expect(screen.getByText(/no events match these filters/i)).toBeInTheDocument();
	});
});
