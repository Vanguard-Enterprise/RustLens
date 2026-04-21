import type { ReactNode } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export interface AdminDetailLayoutProps {
  title: string;
  summary: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
}

export function AdminDetailLayout({
  title,
  summary,
  actions,
  children,
}: AdminDetailLayoutProps) {
  return (
    <div className="grid gap-4 md:grid-cols-[2fr_1fr]">
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {summary}
          {children}
        </CardContent>
      </Card>
      {actions ? (
        <Card>
          <CardHeader>
            <CardTitle>Actions</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">{actions}</CardContent>
        </Card>
      ) : null}
    </div>
  );
}
