"use client";

import { useState } from "react";
import { MoreHorizontalIcon } from "lucide-react";
import type { AdminApiKey } from "@/lib/rustlens-api";
import { formatRelative } from "@/lib/relative-time";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { RotateApiKeyDialog } from "./rotate-api-key-dialog";
import { RevokeApiKeyDialog } from "./revoke-api-key-dialog";

export function ApiKeysTable({
	items,
	csrfToken,
}: {
	items: AdminApiKey[];
	csrfToken: string;
}) {
	const [rotating, setRotating] = useState<{ id: string; name: string } | null>(null);
	const [revoking, setRevoking] = useState<{ id: string; name: string } | null>(null);

	return (
		<>
			<Table>
				<TableHeader>
					<TableRow>
						<TableHead>Name</TableHead>
						<TableHead>Prefix</TableHead>
						<TableHead>Permissions</TableHead>
						<TableHead>Last used</TableHead>
						<TableHead>Expires</TableHead>
						<TableHead className="w-[1%]" />
					</TableRow>
				</TableHeader>
				<TableBody>
					{items.map((k) => (
						<TableRow key={k.id} data-revoked={k.revokedAt !== null}>
							<TableCell>{k.name}</TableCell>
							<TableCell className="font-mono">
								{k.prefix}…{k.last4}
							</TableCell>
							<TableCell>
								{k.revokedAt ? "revoked" : `${k.permissions.length} perms`}
							</TableCell>
							<TableCell>
								{k.lastUsedAt ? formatRelative(k.lastUsedAt) : "—"}
							</TableCell>
							<TableCell>
								{k.expiresAt ? formatRelative(k.expiresAt) : "never"}
							</TableCell>
							<TableCell>
								<DropdownMenu>
									<DropdownMenuTrigger asChild>
										<Button size="icon" variant="ghost">
											<MoreHorizontalIcon className="size-4" />
										</Button>
									</DropdownMenuTrigger>
									<DropdownMenuContent align="end">
										<DropdownMenuItem
											disabled={k.revokedAt !== null}
											onClick={() => setRotating({ id: k.id, name: k.name })}
										>
											Rotate
										</DropdownMenuItem>
										<DropdownMenuItem
											disabled={k.revokedAt !== null}
											className="text-destructive"
											onClick={() => setRevoking({ id: k.id, name: k.name })}
										>
											Revoke
										</DropdownMenuItem>
									</DropdownMenuContent>
								</DropdownMenu>
							</TableCell>
						</TableRow>
					))}
					{items.length === 0 && (
						<TableRow>
							<TableCell
								colSpan={6}
								className="text-center text-muted-foreground"
							>
								No API keys yet.
							</TableCell>
						</TableRow>
					)}
				</TableBody>
			</Table>
			{rotating && (
				<RotateApiKeyDialog
					keyId={rotating.id}
					keyName={rotating.name}
					csrfToken={csrfToken}
					open={true}
					onOpenChange={(o) => !o && setRotating(null)}
				/>
			)}
			{revoking && (
				<RevokeApiKeyDialog
					keyId={revoking.id}
					keyName={revoking.name}
					csrfToken={csrfToken}
					open={true}
					onOpenChange={(o) => !o && setRevoking(null)}
				/>
			)}
		</>
	);
}
