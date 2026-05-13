"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type {
	AdminBmServerInput,
	AdminBmServerItem,
} from "@/lib/rustlens-api";

import { createBmServerAction, updateBmServerAction } from "../actions";

type Props =
	| {
			mode: "create";
			open: boolean;
			onOpenChange: (open: boolean) => void;
			onSuccess: () => void;
			server?: undefined;
	  }
	| {
			mode: "edit";
			open: boolean;
			onOpenChange: (open: boolean) => void;
			onSuccess: () => void;
			server: AdminBmServerItem;
	  };

const EMPTY_INPUT: AdminBmServerInput = {
	bmId: "",
	name: "",
	isVitality: false,
	isAtlas: false,
	isVital: false,
	isModded: false,
	isOfficial: false,
	isBuild: false,
	highFrequency: false,
};

function inputFromServer(server: AdminBmServerItem): AdminBmServerInput {
	return {
		bmId: server.bmId,
		name: server.name,
		isVitality: server.isVitality,
		isAtlas: server.isAtlas,
		isVital: server.isVital,
		isModded: server.isModded,
		isOfficial: server.isOfficial,
		isBuild: server.isBuild,
		highFrequency: server.highFrequency,
	};
}

const CATEGORY_FIELDS: ReadonlyArray<{
	key: keyof Pick<
		AdminBmServerInput,
		"isVitality" | "isAtlas" | "isVital" | "isModded" | "isOfficial" | "isBuild"
	>;
	label: string;
}> = [
	{ key: "isVitality", label: "Vitality" },
	{ key: "isAtlas", label: "Atlas" },
	{ key: "isVital", label: "Vital" },
	{ key: "isModded", label: "Modded" },
	{ key: "isOfficial", label: "Official" },
	{ key: "isBuild", label: "Build" },
];

export function BmServerDialog(props: Props) {
	const { mode, open, onOpenChange, onSuccess } = props;
	const initial = mode === "edit" ? inputFromServer(props.server) : EMPTY_INPUT;
	const [form, setForm] = React.useState<AdminBmServerInput>(initial);
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);
	const [prevOpen, setPrevOpen] = React.useState(open);
	const serverKey = mode === "edit" ? props.server.bmId : "";
	const [prevKey, setPrevKey] = React.useState(serverKey);

	if (prevOpen !== open) {
		setPrevOpen(open);
		if (!open) {
			setForm(initial);
			setReason("");
			setError(null);
			setSubmitting(false);
		}
	}
	if (prevKey !== serverKey) {
		setPrevKey(serverKey);
		setForm(initial);
	}

	const patch = (next: Partial<AdminBmServerInput>) =>
		setForm((prev) => ({ ...prev, ...next }));

	const submit = async () => {
		if (!form.bmId.trim() || !form.name.trim()) return;
		setSubmitting(true);
		setError(null);
		const res =
			mode === "create"
				? await createBmServerAction(form, reason)
				: await updateBmServerAction(props.server.bmId, form, reason);
		setSubmitting(false);
		if (!res.ok) {
			setError(res.error);
			return;
		}
		onOpenChange(false);
		onSuccess();
	};

	const title = mode === "create" ? "Add tracked server" : "Edit tracked server";
	const description =
		mode === "create"
			? "Register a new BattleMetrics server target. The backend will start polling it on the next rolling tick."
			: "Update the tracked server's BattleMetrics ID, display name, categories, or polling tier.";

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>{title}</DialogTitle>
					<DialogDescription>{description}</DialogDescription>
				</DialogHeader>
				<div className="flex flex-col gap-3">
					<div className="grid grid-cols-2 gap-3">
						<div className="flex flex-col gap-1">
							<Label htmlFor="bm-server-name">Name</Label>
							<Input
								id="bm-server-name"
								placeholder="My RustLens Server"
								value={form.name}
								onChange={(e) => patch({ name: e.target.value })}
								autoComplete="off"
								spellCheck={false}
							/>
						</div>
						<div className="flex flex-col gap-1">
							<Label htmlFor="bm-server-bmid">BattleMetrics ID</Label>
							<Input
								id="bm-server-bmid"
								placeholder="1234567"
								inputMode="numeric"
								value={form.bmId}
								onChange={(e) => patch({ bmId: e.target.value })}
								autoComplete="off"
								spellCheck={false}
							/>
						</div>
					</div>
					<div className="flex flex-col gap-1">
						<span className="text-xs font-medium uppercase text-muted-foreground">
							Categories
						</span>
						<div className="grid grid-cols-3 gap-2">
							{CATEGORY_FIELDS.map(({ key, label }) => (
								<label
									key={key}
									className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm"
								>
									<Checkbox
										checked={form[key]}
										onCheckedChange={(v) =>
											patch({ [key]: Boolean(v) } as Partial<AdminBmServerInput>)
										}
									/>
									{label}
								</label>
							))}
						</div>
					</div>
					<label className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm">
						<Checkbox
							checked={form.highFrequency}
							onCheckedChange={(v) =>
								patch({ highFrequency: Boolean(v) })
							}
						/>
						<span className="flex flex-col">
							<span>High frequency polling</span>
							<span className="text-xs text-muted-foreground">
								Polled 3× more often than standard servers in the rolling
								schedule.
							</span>
						</span>
					</label>
					<div className="flex flex-col gap-1">
						<Label htmlFor="bm-server-reason">Reason (optional)</Label>
						<Input
							id="bm-server-reason"
							placeholder="Why is this being added/changed?"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
						/>
					</div>
				</div>
				{error ? (
					<div className="text-sm text-destructive">{error}</div>
				) : null}
				<DialogFooter>
					<Button
						variant="ghost"
						onClick={() => onOpenChange(false)}
						disabled={submitting}
					>
						Cancel
					</Button>
					<Button
						onClick={submit}
						disabled={submitting || !form.bmId.trim() || !form.name.trim()}
					>
						{submitting
							? mode === "create"
								? "Adding…"
								: "Saving…"
							: mode === "create"
								? "Add"
								: "Save"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
