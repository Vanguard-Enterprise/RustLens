"use client";

import { ChevronsUpDownIcon, CheckIcon } from "lucide-react";
import * as React from "react";

import { Button } from "@/components/ui/button";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
} from "@/components/ui/command";
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
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { AdminRoleItem } from "@/lib/rustlens-api";
import { cn } from "@/lib/utils";

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	userId: string;
	assignableRoles: AdminRoleItem[];
	canGrantSuperadmin: boolean;
	onConfirm: (userId: string, roleKey: string, reason: string) => Promise<void>;
};

export function GrantRoleDialog({
	open,
	onOpenChange,
	userId,
	assignableRoles,
	canGrantSuperadmin,
	onConfirm,
}: Props) {
	const [roleKey, setRoleKey] = React.useState<string>("");
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);
	const [pickerOpen, setPickerOpen] = React.useState(false);
	const [prevOpen, setPrevOpen] = React.useState(open);

	if (prevOpen !== open) {
		setPrevOpen(open);
		if (!open) {
			setRoleKey("");
			setReason("");
			setError(null);
			setSubmitting(false);
			setPickerOpen(false);
		}
	}

	const selected = assignableRoles.find((r) => r.key === roleKey) ?? null;

	const submit = async () => {
		if (!roleKey || !reason.trim()) return;
		setSubmitting(true);
		setError(null);
		try {
			await onConfirm(userId, roleKey, reason.trim());
			onOpenChange(false);
		} catch (err) {
			setError((err as Error).message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Grant role</DialogTitle>
					<DialogDescription>
						Assign an admin role. Granting <code>superadmin</code> requires admins.grant.
					</DialogDescription>
				</DialogHeader>

				<div className="flex flex-col gap-3">
					<div className="flex flex-col gap-1">
						<Label htmlFor="role-picker">Role</Label>
						<Popover open={pickerOpen} onOpenChange={setPickerOpen}>
							<PopoverTrigger asChild>
								<Button
									id="role-picker"
									variant="outline"
									role="combobox"
									aria-expanded={pickerOpen}
									className="w-full justify-between font-normal"
								>
									<span className={cn(!selected && "text-muted-foreground")}>
										{selected ? selected.name : "Search and pick a role…"}
									</span>
									<ChevronsUpDownIcon className="ml-2 size-4 shrink-0 opacity-50" />
								</Button>
							</PopoverTrigger>
							<PopoverContent
								className="w-[--radix-popover-trigger-width] p-0"
								align="start"
							>
								<Command>
									<CommandInput placeholder="Search by name, key, or description…" />
									<CommandList>
										<CommandEmpty>No roles found.</CommandEmpty>
										<CommandGroup>
											{assignableRoles.map((r) => {
												const disabled =
													r.key === "superadmin" && !canGrantSuperadmin;
												const searchValue = [
													r.name,
													r.key,
													r.description ?? "",
												].join(" ");
												return (
													<CommandItem
														key={r.key}
														value={searchValue}
														disabled={disabled}
														onSelect={() => {
															if (disabled) return;
															setRoleKey(r.key);
															setPickerOpen(false);
														}}
														className="flex items-start gap-2"
													>
														<CheckIcon
															className={cn(
																"mt-0.5 size-4",
																roleKey === r.key ? "opacity-100" : "opacity-0",
															)}
														/>
														<div className="flex min-w-0 flex-col">
															<span className="truncate">{r.name}</span>
															<span className="truncate font-mono text-xs text-muted-foreground">
																{r.key}
															</span>
															{r.description ? (
																<span className="text-xs text-muted-foreground">
																	{r.description}
																</span>
															) : null}
															{disabled ? (
																<span className="text-xs text-amber-600">
																	Requires admins.grant
																</span>
															) : null}
														</div>
													</CommandItem>
												);
											})}
										</CommandGroup>
									</CommandList>
								</Command>
							</PopoverContent>
						</Popover>
					</div>

					<div className="flex flex-col gap-1">
						<Label htmlFor="reason">Reason</Label>
						<Input
							id="reason"
							placeholder="Reason (required, audited)"
							value={reason}
							onChange={(e) => setReason(e.target.value)}
						/>
					</div>

					{error ? <div className="text-sm text-destructive">{error}</div> : null}
				</div>

				<DialogFooter>
					<Button variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
						Cancel
					</Button>
					<Button onClick={submit} disabled={submitting || !roleKey || !reason.trim()}>
						Grant role
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
