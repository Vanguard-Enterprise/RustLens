"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";

import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { searchPlayersAction } from "@/app/_actions/player-search";
import type { PlayerSearchHit } from "@/lib/rustlens-api";

const DEBOUNCE_MS = 250;
const MIN_QUERY_LEN = 2;
const TOP_N = 6;

type ResultsState =
	| { kind: "idle" }
	| { kind: "loading" }
	| { kind: "ok"; hits: PlayerSearchHit[]; mode: "identifier" | "name" }
	| { kind: "forbidden" }
	| { kind: "rate_limited" }
	| { kind: "error" };

export function NavbarPlayerSearch() {
	const [q, setQ] = useState("");
	const [open, setOpen] = useState(false);
	const [state, setState] = useState<ResultsState>({ kind: "idle" });
	const abortRef = useRef<AbortController | null>(null);

	useEffect(() => {
		const trimmed = q.trim();
		if (trimmed.length < MIN_QUERY_LEN) {
			setState({ kind: "idle" });
			return;
		}
		const handle = setTimeout(async () => {
			abortRef.current?.abort();
			const ctl = new AbortController();
			abortRef.current = ctl;
			setState({ kind: "loading" });
			const res = await searchPlayersAction(trimmed, false);
			if (ctl.signal.aborted) return;
			if ("error" in res) {
				if (res.error === "forbidden") setState({ kind: "forbidden" });
				else if (res.error === "rate_limited") setState({ kind: "rate_limited" });
				else setState({ kind: "error" });
				return;
			}
			setState({ kind: "ok", hits: res.items.slice(0, TOP_N), mode: res.mode });
		}, DEBOUNCE_MS);
		return () => clearTimeout(handle);
	}, [q]);

	useEffect(() => {
		const onKey = (e: KeyboardEvent) => {
			if (e.key !== "/") return;
			const target = e.target as HTMLElement | null;
			if (target?.tagName === "INPUT" || target?.tagName === "TEXTAREA" || target?.isContentEditable) return;
			e.preventDefault();
			document.querySelector<HTMLInputElement>("input[data-navbar-player-search]")?.focus();
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, []);

	return (
		<div className="hidden sm:block w-72">
			<Popover open={open && state.kind !== "idle"} onOpenChange={setOpen}>
				<PopoverTrigger asChild>
					<Input
						data-navbar-player-search
						type="search"
						role="searchbox"
						placeholder="Search players  /"
						value={q}
						onChange={(e) => {
							setQ(e.target.value);
							setOpen(true);
						}}
						onFocus={() => setOpen(true)}
					/>
				</PopoverTrigger>
				<PopoverContent align="end" className="w-80 p-0">
					<NavbarSearchResults q={q} state={state} />
				</PopoverContent>
			</Popover>
		</div>
	);
}

function NavbarSearchResults({ q, state }: { q: string; state: ResultsState }) {
	if (state.kind === "loading") return <div className="p-3 text-sm text-muted-foreground">Searching…</div>;
	if (state.kind === "forbidden") {
		return (
			<div className="p-3 text-sm text-muted-foreground">
				Identifier-only search available. Paste a Steam ID, BM ID, or steamcommunity URL.
			</div>
		);
	}
	if (state.kind === "rate_limited") return <div className="p-3 text-sm text-destructive">Rate limited. Try again shortly.</div>;
	if (state.kind === "error") return <div className="p-3 text-sm text-destructive">Search failed.</div>;
	if (state.kind !== "ok") return null;

	if (state.hits.length === 0) {
		return <div className="p-3 text-sm text-muted-foreground">No players found.</div>;
	}

	return (
		<div className="flex flex-col">
			<ul className="max-h-80 overflow-auto py-1">
				{state.hits.map((hit) => (
					<li key={hit.steamId}>
						<Link
							href={`/player/${encodeURIComponent(hit.steamId)}`}
							className="flex items-center gap-3 px-3 py-2 hover:bg-muted"
						>
							{hit.avatarUrl ? (
								<img alt="" src={hit.avatarUrl} className="h-8 w-8 rounded" />
							) : (
								<div className="h-8 w-8 rounded bg-muted" />
							)}
							<div className="flex flex-col text-sm">
								<span className="font-medium">{hit.bmName ?? hit.steamId}</span>
								<span className="text-xs text-muted-foreground">
									{hit.currentServer ? hit.currentServer.name : hit.isOnline ? "online" : "offline"}
								</span>
							</div>
						</Link>
					</li>
				))}
			</ul>
			<Link
				href={`/search?q=${encodeURIComponent(q)}`}
				className="border-t px-3 py-2 text-xs font-medium text-muted-foreground hover:bg-muted"
			>
				View all results →
			</Link>
		</div>
	);
}
