"use client";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useState, useTransition } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuCheckboxItem,
	DropdownMenuContent,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

type Option = { value: string; label: string };

const STATUS_OPTIONS: Option[] = [
	{ value: "active", label: "Active" },
	{ value: "suspended", label: "Suspended" },
	{ value: "deleted", label: "Deleted" },
	{ value: "any", label: "Any" },
];
const TRISTATE: Option[] = [
	{ value: "any", label: "Any" },
	{ value: "linked", label: "Linked" },
	{ value: "unlinked", label: "Unlinked" },
];
const VERIFIED: Option[] = [
	{ value: "any", label: "Any" },
	{ value: "yes", label: "Verified" },
	{ value: "no", label: "Unverified" },
];

export function UsersFilterBar({ roleOptions }: { roleOptions: Option[] }) {
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
	const currentStatus = sp.get("status") ?? "active";
	const currentDiscord = sp.get("discord") ?? "any";
	const currentSteam = sp.get("steam") ?? "any";
	const currentVerified = sp.get("emailVerified") ?? "any";

	return (
		<div className="flex flex-wrap gap-2 items-center mb-4">
			<Input
				aria-label="Search users"
				placeholder="Search name, email, id, discord..."
				className="max-w-xs"
				value={q}
				onChange={(e) => setQ(e.target.value)}
				onKeyDown={(e) => {
					if (e.key === "Enter") updateParam("q", q || null);
				}}
			/>
			<DropdownMenu>
				<DropdownMenuTrigger asChild>
					<Button variant="outline">Role</Button>
				</DropdownMenuTrigger>
				<DropdownMenuContent>
					{roleOptions.map((o) => (
						<DropdownMenuCheckboxItem
							key={o.value}
							checked={currentRoles.includes(o.value)}
							onCheckedChange={(v) => {
								const next = v
									? [...currentRoles, o.value]
									: currentRoles.filter((x) => x !== o.value);
								updateParam("role", next.length ? next : null);
							}}
						>
							{o.label}
						</DropdownMenuCheckboxItem>
					))}
				</DropdownMenuContent>
			</DropdownMenu>
			<SingleSelect
				label="Status"
				value={currentStatus}
				options={STATUS_OPTIONS}
				onChange={(v) => updateParam("status", v === "active" ? null : v)}
			/>
			<SingleSelect
				label="Discord"
				value={currentDiscord}
				options={TRISTATE}
				onChange={(v) => updateParam("discord", v === "any" ? null : v)}
			/>
			<SingleSelect
				label="Steam"
				value={currentSteam}
				options={TRISTATE}
				onChange={(v) => updateParam("steam", v === "any" ? null : v)}
			/>
			<SingleSelect
				label="Verified"
				value={currentVerified}
				options={VERIFIED}
				onChange={(v) => updateParam("emailVerified", v === "any" ? null : v)}
			/>
			<Button variant="ghost" onClick={() => router.replace(pathname)}>
				Clear
			</Button>
		</div>
	);
}

function SingleSelect({
	label,
	value,
	options,
	onChange,
}: {
	label: string;
	value: string;
	options: Option[];
	onChange: (v: string) => void;
}) {
	return (
		<DropdownMenu>
			<DropdownMenuTrigger asChild>
				<Button variant="outline">
					{label}: {options.find((o) => o.value === value)?.label ?? value}
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent>
				{options.map((o) => (
					<DropdownMenuCheckboxItem
						key={o.value}
						checked={value === o.value}
						onCheckedChange={() => onChange(o.value)}
					>
						{o.label}
					</DropdownMenuCheckboxItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
