import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import { AuditToolbar } from "../audit-toolbar";

const baseQuery = {
	q: undefined,
	since: "7d" as const,
	result: undefined,
	eventType: [],
	actor: null,
	target: null,
};

describe("AuditToolbar", () => {
	it("preset chip click emits since key", () => {
		const onChange = vi.fn();
		render(<AuditToolbar query={baseQuery} eventTypes={["users.suspend"]} onChange={onChange} />);
		fireEvent.click(screen.getByRole("button", { name: /filters/i }));
		fireEvent.click(screen.getByRole("button", { name: /last 24h/i }));
		expect(onChange).toHaveBeenCalledWith({ since: "24h" });
	});

	it("event type pill toggles", () => {
		const onChange = vi.fn();
		render(<AuditToolbar query={baseQuery} eventTypes={["users.suspend", "users.delete"]} onChange={onChange} />);
		fireEvent.click(screen.getByRole("button", { name: /filters/i }));
		fireEvent.click(screen.getByRole("button", { name: "users.suspend" }));
		expect(onChange).toHaveBeenCalledWith({ eventType: ["users.suspend"] });
	});

	it("clear-all resets to defaults", () => {
		const onChange = vi.fn();
		render(
			<AuditToolbar
				query={{ ...baseQuery, since: "24h", result: "error", eventType: ["users.suspend"] }}
				eventTypes={["users.suspend"]}
				onChange={onChange}
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: /clear filters/i }));
		expect(onChange).toHaveBeenCalledWith({
			since: "7d",
			result: undefined,
			eventType: [],
			actorUserId: undefined,
			targetUserId: undefined,
			actorLabel: null,
			targetLabel: null,
		});
	});
});
