"use client";

import * as React from "react";
import { ChevronDownIcon, Loader2Icon, XIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
} from "@/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { AdminUserItem } from "@/lib/rustlens-api";
import { searchAdminUsersAction } from "../actions";

export type UserComboboxValue = {
	id: string;
	displayName: string;
	avatarUrl: string | null;
};

type Props = {
	label: string;
	value: UserComboboxValue | null;
	onSelect: (v: UserComboboxValue) => void;
	onClear: () => void;
};

const DEBOUNCE_MS = 120;
const MIN_QUERY_LEN = 1;

export function UserCombobox({ label, value, onSelect, onClear }: Props) {
	const [open, setOpen] = React.useState(false);
	const [text, setText] = React.useState("");
	// Per-instance cache so retyping or backspacing into a previous query
	// renders instantly. Cleared when the popover is closed.
	const [cache, setCache] = React.useState<Map<string, AdminUserItem[]>>(
		() => new Map(),
	);
	const [errorQuery, setErrorQuery] = React.useState<string | null>(null);
	const [loadingQuery, setLoadingQuery] = React.useState<string | null>(null);
	const reqIdRef = React.useRef(0);

	const trimmed = text.trim();
	const tooShort = trimmed.length < MIN_QUERY_LEN;
	const cached = !tooShort ? cache.get(trimmed) : undefined;
	const displayResults: AdminUserItem[] = tooShort ? [] : cached ?? [];
	const displayLoading = !tooShort && !cached && loadingQuery === trimmed;
	const displayError = !tooShort && !cached && errorQuery === trimmed;

	React.useEffect(() => {
		if (!open || tooShort || cached) return;
		const myReq = ++reqIdRef.current;
		const t = setTimeout(async () => {
			setLoadingQuery(trimmed);
			try {
				const res = await searchAdminUsersAction(trimmed);
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
	}, [trimmed, open, tooShort, cached]);

	const handleOpenChange = (next: boolean) => {
		setOpen(next);
		if (!next) {
			setText("");
			setCache(new Map());
			setErrorQuery(null);
			setLoadingQuery(null);
		}
	};

	if (value) {
		return (
			<div className="inline-flex items-center gap-1.5 rounded-md border bg-muted px-2 py-1 text-sm">
				{value.avatarUrl ? (
					// eslint-disable-next-line @next/next/no-img-element
					<img src={value.avatarUrl} alt="" className="size-5 rounded-full object-cover" />
				) : (
					<div className="size-5 rounded-full bg-background" />
				)}
				<span className="font-medium">{value.displayName}</span>
				<Button
					variant="ghost"
					size="icon"
					className="size-5"
					aria-label={`Clear ${label}`}
					onClick={onClear}
				>
					<XIcon className="size-3" />
				</Button>
			</div>
		);
	}

	return (
		<Popover open={open} onOpenChange={handleOpenChange}>
			<PopoverTrigger asChild>
				<Button
					variant="outline"
					size="sm"
					className="justify-between gap-2"
					aria-label={`Search admin user (${label})`}
				>
					<span className="text-muted-foreground">Search…</span>
					<ChevronDownIcon className="size-4" />
				</Button>
			</PopoverTrigger>
			<PopoverContent align="start" className="w-80 p-0">
				<Command shouldFilter={false}>
					<div className="relative">
						<CommandInput
							placeholder={`Search ${label.toLowerCase()}…`}
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
									{displayResults.map((it) => {
										const handle = it.discordUsername;
										return (
											<CommandItem
												key={it.user.id}
												value={it.user.id}
												onSelect={() => {
													onSelect({
														id: it.user.id,
														displayName: it.user.displayName,
														avatarUrl: it.user.avatarUrl ?? null,
													});
													setOpen(false);
													setText("");
												}}
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
													{handle ? (
														<span className="truncate text-xs text-muted-foreground">
															@{handle}
														</span>
													) : null}
												</div>
											</CommandItem>
										);
									})}
								</CommandGroup>
							</>
						)}
					</CommandList>
				</Command>
			</PopoverContent>
		</Popover>
	);
}
