"use client";

import { CheckIcon, ChevronsUpDownIcon, Loader2Icon } from "lucide-react";
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
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import type { AdminRoleItem, AdminUserItem } from "@/lib/rustlens-api";
import { cn } from "@/lib/utils";

type Stage = "search" | "role";

type SelectedUser = {
	id: string;
	displayName: string;
	discordUsername: string;
	avatarUrl: string;
};

type Props = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	assignableRoles: AdminRoleItem[];
	canGrantSuperadmin: boolean;
	onSearch: (q: string) => Promise<AdminUserItem[]>;
	onConfirm: (userId: string, roleKey: string, reason: string) => Promise<void>;
};

const DEBOUNCE_MS = 120;
const MIN_QUERY_LEN = 1;

export function AddAdminUserDialog({
	open,
	onOpenChange,
	assignableRoles,
	canGrantSuperadmin,
	onSearch,
	onConfirm,
}: Props) {
	const [stage, setStage] = React.useState<Stage>("search");
	const [selectedUser, setSelectedUser] = React.useState<SelectedUser | null>(
		null,
	);
	const [roleKey, setRoleKey] = React.useState<string>("");
	const [reason, setReason] = React.useState("");
	const [submitting, setSubmitting] = React.useState(false);
	const [error, setError] = React.useState<string | null>(null);
	const [pickerOpen, setPickerOpen] = React.useState(false);

	// Search-stage state.
	const [text, setText] = React.useState("");
	const [cache, setCache] = React.useState<Map<string, AdminUserItem[]>>(
		() => new Map(),
	);
	const [loadingQuery, setLoadingQuery] = React.useState<string | null>(null);
	const [errorQuery, setErrorQuery] = React.useState<string | null>(null);
	const reqIdRef = React.useRef(0);

	const [prevOpen, setPrevOpen] = React.useState(open);
	if (prevOpen !== open) {
		setPrevOpen(open);
		if (!open) {
			setStage("search");
			setSelectedUser(null);
			setRoleKey("");
			setReason("");
			setError(null);
			setSubmitting(false);
			setPickerOpen(false);
			setText("");
			setCache(new Map());
			setLoadingQuery(null);
			setErrorQuery(null);
		}
	}

	const trimmed = text.trim();
	const tooShort = trimmed.length < MIN_QUERY_LEN;
	const cached = !tooShort ? cache.get(trimmed) : undefined;
	const displayResults: AdminUserItem[] = tooShort ? [] : cached ?? [];
	const displayLoading = !tooShort && !cached && loadingQuery === trimmed;
	const displayError = !tooShort && !cached && errorQuery === trimmed;

	React.useEffect(() => {
		if (stage !== "search" || tooShort || cached) return;
		const myReq = ++reqIdRef.current;
		const t = setTimeout(async () => {
			setLoadingQuery(trimmed);
			try {
				const res = await onSearch(trimmed);
				if (myReq !== reqIdRef.current) return;
				setCache((prev) => new Map(prev).set(trimmed, res));
				setErrorQuery((prev) => (prev === trimmed ? null : prev));
			} catch {
				if (myReq !== reqIdRef.current) return;
				setErrorQuery(trimmed);
			} finally {
				if (myReq === reqIdRef.current) {
					setLoadingQuery((prev) => (prev === trimmed ? null : prev));
				}
			}
		}, DEBOUNCE_MS);
		return () => clearTimeout(t);
	}, [trimmed, stage, tooShort, cached, onSearch]);

	const pickUser = (it: AdminUserItem) => {
		setSelectedUser({
			id: it.user.id,
			displayName: it.user.displayName || it.user.id,
			discordUsername: it.discordUsername,
			avatarUrl: it.user.avatarUrl ?? "",
		});
		setStage("role");
	};

	const backToSearch = () => {
		setStage("search");
		setSelectedUser(null);
		setRoleKey("");
		setReason("");
		setError(null);
	};

	const selectedRole = assignableRoles.find((r) => r.key === roleKey) ?? null;

	const submit = async () => {
		if (!selectedUser || !roleKey || !reason.trim()) return;
		setSubmitting(true);
		setError(null);
		try {
			await onConfirm(selectedUser.id, roleKey, reason.trim());
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
					<DialogTitle>Add user</DialogTitle>
					<DialogDescription>
						{stage === "search"
							? "Search for a user by name, Discord handle, or ID."
							: "Pick an admin role and reason. Granting superadmin requires admins.grant."}
					</DialogDescription>
				</DialogHeader>

				{stage === "search" ? (
					<div className="flex flex-col gap-3">
						<Command shouldFilter={false}>
							<div className="relative">
								<CommandInput
									placeholder="Search users…"
									value={text}
									onValueChange={setText}
								/>
								{displayLoading ? (
									<Loader2Icon className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 animate-spin text-muted-foreground" />
								) : null}
							</div>
							<CommandList className="max-h-80">
								{displayError ? (
									<div className="p-3 text-xs text-muted-foreground">
										Search unavailable — try again.
									</div>
								) : (
									<>
										<CommandEmpty>
											{tooShort
												? "Type to search…"
												: displayLoading
													? "Searching…"
													: "No matches."}
										</CommandEmpty>
										<CommandGroup>
											{displayResults.map((it) => (
												<CommandItem
													key={it.user.id}
													value={it.user.id}
													onSelect={() => pickUser(it)}
													className="gap-2.5 py-2"
												>
													{it.user.avatarUrl ? (
														// eslint-disable-next-line @next/next/no-img-element
														<img
															src={it.user.avatarUrl}
															alt=""
															className="size-7 shrink-0 rounded-full object-cover"
														/>
													) : (
														<div className="size-7 shrink-0 rounded-full bg-muted" />
													)}
													<div className="flex min-w-0 flex-col leading-tight">
														<span className="truncate text-sm font-medium">
															{it.user.displayName || it.user.id}
														</span>
														{it.discordUsername ? (
															<span className="truncate text-xs text-muted-foreground">
																@{it.discordUsername}
															</span>
														) : null}
													</div>
												</CommandItem>
											))}
										</CommandGroup>
									</>
								)}
							</CommandList>
						</Command>
					</div>
				) : (
					<div className="flex flex-col gap-3">
						<div className="flex items-center justify-between rounded-md border bg-muted/40 p-2">
							<div className="flex items-center gap-2">
								{selectedUser?.avatarUrl ? (
									// eslint-disable-next-line @next/next/no-img-element
									<img
										src={selectedUser.avatarUrl}
										alt=""
										className="size-7 rounded-full object-cover"
									/>
								) : (
									<div className="size-7 rounded-full bg-background" />
								)}
								<div className="flex min-w-0 flex-col leading-tight">
									<span className="text-xs uppercase text-muted-foreground">
										Selected user
									</span>
									<span className="truncate text-sm font-medium">
										{selectedUser?.displayName}
									</span>
								</div>
							</div>
							<Button variant="ghost" size="sm" onClick={backToSearch}>
								Change
							</Button>
						</div>

						<div className="flex flex-col gap-1">
							<Label htmlFor="add-user-role-picker">Role</Label>
							<Popover open={pickerOpen} onOpenChange={setPickerOpen}>
								<PopoverTrigger asChild>
									<Button
										id="add-user-role-picker"
										variant="outline"
										role="combobox"
										aria-expanded={pickerOpen}
										aria-label="Role"
										className="w-full justify-between font-normal"
									>
										<span
											className={cn(!selectedRole && "text-muted-foreground")}
										>
											{selectedRole
												? selectedRole.name
												: "Search and pick a role…"}
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
																	roleKey === r.key
																		? "opacity-100"
																		: "opacity-0",
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
							<Label htmlFor="add-user-reason">Reason</Label>
							<Input
								id="add-user-reason"
								placeholder="Reason (required, audited)"
								value={reason}
								onChange={(e) => setReason(e.target.value)}
							/>
						</div>

						{error ? (
							<div className="text-sm text-destructive">{error}</div>
						) : null}
					</div>
				)}

				<DialogFooter>
					<Button
						variant="ghost"
						onClick={() => onOpenChange(false)}
						disabled={submitting}
					>
						Cancel
					</Button>
					{stage === "role" ? (
						<Button
							onClick={submit}
							disabled={submitting || !roleKey || !reason.trim()}
						>
							Grant role
						</Button>
					) : null}
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
