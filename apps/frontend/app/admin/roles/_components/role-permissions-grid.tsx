"use client";

import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import type { AdminPermissionItem } from "@/lib/rustlens-api";

type Props = {
    catalog: AdminPermissionItem[];
    selected: Set<string>;
    onToggle: (key: string, on: boolean) => void;
    disabled?: boolean;
};

function groupByNamespace(catalog: AdminPermissionItem[]) {
    const groups = new Map<string, AdminPermissionItem[]>();
    for (const p of catalog) {
        const ns = p.key.split(".")[0] ?? "other";
        const arr = groups.get(ns);
        if (arr) arr.push(p);
        else groups.set(ns, [p]);
    }
    // Stable order: alphabetical group, then alphabetical within group.
    return [...groups.entries()]
        .sort((a, b) => a[0].localeCompare(b[0]))
        .map(([ns, items]) => ({
            ns,
            items: [...items].sort((a, b) => a.key.localeCompare(b.key)),
        }));
}

export function RolePermissionsGrid({ catalog, selected, onToggle, disabled = false }: Props) {
    const groups = groupByNamespace(catalog);
    return (
        <div className="flex flex-col gap-6">
            {groups.map((g) => (
                <section key={g.ns} className="flex flex-col gap-2">
                    <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
                        {g.ns}
                    </h3>
                    <ul className="flex flex-col gap-2">
                        {g.items.map((p) => {
                            const id = `perm-${p.key}`;
                            return (
                                <li key={p.key} className="flex items-start gap-3">
                                    <Checkbox
                                        id={id}
                                        checked={selected.has(p.key)}
                                        disabled={disabled}
                                        onCheckedChange={(v) => onToggle(p.key, v === true)}
                                    />
                                    <Label htmlFor={id} className="flex flex-col items-start gap-0.5 leading-tight">
                                        <code className="text-xs font-mono">{p.key}</code>
                                        <span className="text-xs text-muted-foreground">{p.description}</span>
                                    </Label>
                                </li>
                            );
                        })}
                    </ul>
                </section>
            ))}
        </div>
    );
}
