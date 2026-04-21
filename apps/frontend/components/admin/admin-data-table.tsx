"use client";

import { useRouter, useSearchParams, usePathname } from "next/navigation";
import { type ReactNode, useMemo } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Input } from "@/components/ui/input";

export interface AdminTableColumn<T> {
  header: string;
  cell: (row: T) => ReactNode;
  key: string;
}

export interface AdminDataTableFilter {
  key: string;
  label: string;
  placeholder?: string;
}

export interface AdminDataTableProps<T> {
  rows: T[];
  columns: AdminTableColumn<T>[];
  filters?: AdminDataTableFilter[];
  emptyMessage?: string;
  rowKey: (row: T) => string;
}

export function AdminDataTable<T>({
  rows,
  columns,
  filters = [],
  emptyMessage = "No rows",
  rowKey,
}: AdminDataTableProps<T>) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const values = useMemo(() => {
    const v: Record<string, string> = {};
    for (const f of filters) v[f.key] = searchParams.get(f.key) ?? "";
    return v;
  }, [filters, searchParams]);

  const setFilter = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams.toString());
    if (value) next.set(key, value);
    else next.delete(key);
    router.replace(`${pathname}?${next.toString()}`);
  };

  return (
    <div className="space-y-4">
      {filters.length ? (
        <div className="flex flex-wrap gap-2">
          {filters.map((f) => (
            <Input
              key={f.key}
              value={values[f.key]}
              onChange={(e) => setFilter(f.key, e.target.value)}
              placeholder={f.placeholder ?? f.label}
              className="max-w-xs"
            />
          ))}
        </div>
      ) : null}
      <Table>
        <TableHeader>
          <TableRow>
            {columns.map((c) => (
              <TableHead key={c.key}>{c.header}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={columns.length} className="text-muted-foreground">
                {emptyMessage}
              </TableCell>
            </TableRow>
          ) : (
            rows.map((row) => (
              <TableRow key={rowKey(row)}>
                {columns.map((c) => (
                  <TableCell key={c.key}>{c.cell(row)}</TableCell>
                ))}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}
