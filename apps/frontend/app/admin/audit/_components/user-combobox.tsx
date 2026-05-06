"use client";

import * as React from "react";
import { ChevronDownIcon, XIcon } from "lucide-react";

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

export function UserCombobox({ label, value, onSelect, onClear }: Props) {
	const [open, setOpen] = React.useState(false);
	const [text, setText] = React.useState("");
	const [results, setResults] = React.useState<AdminUserItem[]>([]);
	const [error, setError] = React.useState(false);

	React.useEffect(() => {
		const t = setTimeout(async () => {
			setError(false);
			try {
				setResults(await searchAdminUsersAction(text));
			} catch {
				setResults([]);
				setError(true);
			}
		}, 250);
		return () => clearTimeout(t);
	}, [text]);

	if (value) {
		return (
			<div className="inline-flex items-center gap-1 rounded border bg-muted px-2 py-1 text-sm">
				{value.avatarUrl ? (
					// eslint-disable-next-line @next/next/no-img-element
					<img src={value.avatarUrl} alt="" className="size-5 rounded-full" />
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
		<Popover open={open} onOpenChange={setOpen}>
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
			<PopoverContent align="start" className="w-72 p-0">
				<Command shouldFilter={false}>
					<CommandInput placeholder={`Search ${label.toLowerCase()}…`} value={text} onValueChange={setText} />
					<CommandList>
						{error ? (
							<div className="p-2 text-xs text-muted-foreground">Search unavailable — try again.</div>
						) : (
							<>
								<CommandEmpty>{text.length < 2 ? "Type to search…" : "No matches."}</CommandEmpty>
								<CommandGroup>
									{results.map((it) => (
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
										>
											<div className="flex items-center gap-2">
												{it.user.avatarUrl ? (
													// eslint-disable-next-line @next/next/no-img-element
													<img src={it.user.avatarUrl} alt="" className="size-5 rounded-full" />
												) : (
													<div className="size-5 rounded-full bg-muted" />
												)}
												<span>{it.user.displayName}</span>
												<span className="text-xs text-muted-foreground">
													@{it.discordUsername || "—"}
												</span>
											</div>
										</CommandItem>
									))}
								</CommandGroup>
							</>
						)}
					</CommandList>
				</Command>
			</PopoverContent>
		</Popover>
	);
}
