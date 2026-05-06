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
	const [results, setResults] = React.useState<AdminUserItem[]>([]);
	const [error, setError] = React.useState(false);
	const [loading, setLoading] = React.useState(false);

	// Per-instance cache so retyping or backspacing into a previous query
	// renders instantly. Cleared when the popover is closed.
	const cacheRef = React.useRef<Map<string, AdminUserItem[]>>(new Map());
	const reqIdRef = React.useRef(0);

	React.useEffect(() => {
		if (!open) return;
		const trimmed = text.trim();
		if (trimmed.length < MIN_QUERY_LEN) {
			setResults([]);
			setLoading(false);
			setError(false);
			return;
		}
		const cached = cacheRef.current.get(trimmed);
		if (cached) {
			setResults(cached);
			setLoading(false);
			setError(false);
			return;
		}
		setLoading(true);
		const myReq = ++reqIdRef.current;
		const t = setTimeout(async () => {
			try {
				const res = await searchAdminUsersAction(trimmed);
				if (myReq !== reqIdRef.current) return;
				cacheRef.current.set(trimmed, res);
				setResults(res);
				setError(false);
			} catch {
				if (myReq !== reqIdRef.current) return;
				setResults([]);
				setError(true);
			} finally {
				if (myReq === reqIdRef.current) setLoading(false);
			}
		}, DEBOUNCE_MS);
		return () => clearTimeout(t);
	}, [text, open]);

	const handleOpenChange = (next: boolean) => {
		setOpen(next);
		if (!next) {
			setText("");
			setResults([]);
			cacheRef.current.clear();
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
						{loading ? (
							<Loader2Icon className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 animate-spin text-muted-foreground" />
						) : null}
					</div>
					<CommandList className="max-h-80">
						{error ? (
							<div className="p-3 text-xs text-muted-foreground">
								Search unavailable — try again.
							</div>
						) : (
							<>
								<CommandEmpty>
									{text.trim().length < MIN_QUERY_LEN
										? "Type to search…"
										: loading
										? "Searching…"
										: "No matches."}
								</CommandEmpty>
								<CommandGroup>
									{results.map((it) => {
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
