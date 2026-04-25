"use client";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useId, useState, useTransition } from "react";
import { FilterIcon, SearchIcon } from "lucide-react";
import {
	InputGroup,
	InputGroupAddon,
	InputGroupInput,
} from "@/components/ui/input-group";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuCheckboxItem,
	DropdownMenuContent,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import {
	ToggleGroup,
	ToggleGroupItem,
} from "@/components/ui/toggle-group";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";

type Option = { value: string; label: string };

const STATUS_OPTIONS: Option[] = [
	{ value: "any", label: "Any" },
	{ value: "active", label: "Active" },
	{ value: "suspended", label: "Suspended" },
	{ value: "deleted", label: "Deleted" },
];
const TRISTATE: Option[] = [
	{ value: "any", label: "Any" },
	{ value: "linked", label: "Linked" },
	{ value: "unlinked", label: "Unlinked" },
];

export function UsersFilterBar({
	roleOptions,
}: {
	roleOptions: Option[];
}) {
	const router = useRouter();
	const pathname = usePathname();
	const sp = useSearchParams();
	const [, startTransition] = useTransition();
	const [q, setQ] = useState(sp.get("q") ?? "");

	const updateParam = useCallback(
		(key: string, value: string | string[] | null) => {
			const next = new URLSearchParams(sp.toString());
			next.delete(key);
			if (Array.isArray(value)) value.forEach((v) => next.append(key, v));
			else if (value) next.set(key, value);
			next.delete("page"); // reset to page 1 when filters change
			startTransition(() => router.replace(`${pathname}?${next.toString()}`));
		},
		[sp, pathname, router],
	);

	const currentRoles = sp.getAll("role");
	const currentStatus = sp.get("status") ?? "any";
	const currentDiscord = sp.get("discord") ?? "any";
	const currentSteam = sp.get("steam") ?? "any";

	// Badge counts only filters housed inside the popover — not the search term.
	const activeFilterCount =
		(sp.get("status") ? 1 : 0) +
		(sp.get("discord") ? 1 : 0) +
		(sp.get("steam") ? 1 : 0) +
		currentRoles.length;

	// Clear is enabled when anything — filters OR search — is active.
	const hasAnything = activeFilterCount > 0 || q.length > 0;

	return (
		<div className="flex flex-wrap items-center gap-2">
			<InputGroup className="h-9 max-w-xs flex-1 min-w-[14rem]">
				<InputGroupAddon>
					<SearchIcon aria-hidden="true" />
				</InputGroupAddon>
				<InputGroupInput
					aria-label="Search users"
					placeholder="Search name, id, Discord…"
					value={q}
					onChange={(e) => setQ(e.target.value)}
					onKeyDown={(e) => {
						if (e.key === "Enter") updateParam("q", q || null);
					}}
				/>
			</InputGroup>

			<Popover>
				<PopoverTrigger asChild>
					<Button
						variant="outline"
						size="sm"
						aria-label="Filter users"
					>
						<FilterIcon aria-hidden="true" />
						Filter
						{activeFilterCount > 0 ? (
							<Badge variant="secondary" className="ml-1">
								{activeFilterCount}
							</Badge>
						) : null}
					</Button>
				</PopoverTrigger>
				<PopoverContent align="start" className="w-[360px] p-0">
					<div className="flex flex-col gap-3 p-3">
						<FilterRow label="Role">
							<RoleDropdown
								options={roleOptions}
								selected={currentRoles}
								onChange={(next) =>
									updateParam("role", next.length ? next : null)
								}
							/>
						</FilterRow>

						<Separator />

						<FilterRow label="Status">
							{(labelId) => (
								<FilterToggleGroup
									labelledBy={labelId}
									value={currentStatus}
									options={STATUS_OPTIONS}
									onChange={(v) =>
										updateParam("status", v === "any" ? null : v)
									}
								/>
							)}
						</FilterRow>

						<FilterRow label="Discord">
							{(labelId) => (
								<FilterToggleGroup
									labelledBy={labelId}
									value={currentDiscord}
									options={TRISTATE}
									onChange={(v) =>
										updateParam("discord", v === "any" ? null : v)
									}
								/>
							)}
						</FilterRow>

						<FilterRow label="Steam">
							{(labelId) => (
								<FilterToggleGroup
									labelledBy={labelId}
									value={currentSteam}
									options={TRISTATE}
									onChange={(v) =>
										updateParam("steam", v === "any" ? null : v)
									}
								/>
							)}
						</FilterRow>
					</div>

					<Separator />

					<div className="flex justify-end p-2">
						<Button
							variant="ghost"
							size="sm"
							disabled={!hasAnything}
							onClick={() => {
								setQ("");
								router.replace(pathname);
							}}
						>
							Clear
						</Button>
					</div>
				</PopoverContent>
			</Popover>
		</div>
	);
}

function FilterRow({
	label,
	children,
}: {
	label: string;
	children: React.ReactNode | ((labelId: string) => React.ReactNode);
}) {
	const labelId = useId();
	return (
		<div className="flex flex-col gap-1.5">
			<span id={labelId} className="text-xs font-medium text-muted-foreground">
				{label}
			</span>
			{typeof children === "function" ? children(labelId) : children}
		</div>
	);
}

function FilterToggleGroup({
	labelledBy,
	value,
	options,
	onChange,
}: {
	labelledBy: string;
	value: string;
	options: Option[];
	onChange: (v: string) => void;
}) {
	return (
		<ToggleGroup
			type="single"
			value={value}
			onValueChange={(v) => {
				// Treat empty string (user clicked the already-selected item) as "any".
				onChange(v || "any");
			}}
			aria-labelledby={labelledBy}
			className="justify-start"
		>
			{options.map((o) => (
				<ToggleGroupItem key={o.value} value={o.value} aria-label={o.label}>
					{o.label}
				</ToggleGroupItem>
			))}
		</ToggleGroup>
	);
}

function RoleDropdown({
	options,
	selected,
	onChange,
}: {
	options: Option[];
	selected: string[];
	onChange: (next: string[]) => void;
}) {
	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<Button variant="outline" size="sm" className="justify-start">
					Select roles
					{selected.length > 0 ? (
						<Badge variant="secondary" className="ml-1">
							{selected.length}
						</Badge>
					) : null}
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="start">
				<DropdownMenuLabel>Filter by role</DropdownMenuLabel>
				<DropdownMenuSeparator />
				{options.map((o) => (
					<DropdownMenuCheckboxItem
						key={o.value}
						checked={selected.includes(o.value)}
						onCheckedChange={(v) => {
							const next = v
								? [...selected, o.value]
								: selected.filter((x) => x !== o.value);
							onChange(next);
						}}
					>
						{o.label}
					</DropdownMenuCheckboxItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
