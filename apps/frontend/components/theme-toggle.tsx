"use client";

import * as React from "react";
import { useTheme } from "next-themes";
import { MonitorIcon, MoonIcon, SunIcon } from "lucide-react";

import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

function useIsClient() {
	return React.useSyncExternalStore(
		() => () => {},
		() => true,
		() => false,
	);
}

export function ThemeToggle() {
	const { theme, setTheme } = useTheme();
	const isClient = useIsClient();

	const value = isClient ? theme ?? "system" : "system";

	return (
		<ToggleGroup
			type="single"
			variant="outline"
			size="sm"
			value={value}
			onValueChange={(v) => {
				if (v) setTheme(v);
			}}
			aria-label="Theme"
			suppressHydrationWarning
		>
			<ToggleGroupItem value="light" aria-label="Light">
				<SunIcon aria-hidden="true" />
			</ToggleGroupItem>
			<ToggleGroupItem value="dark" aria-label="Dark">
				<MoonIcon aria-hidden="true" />
			</ToggleGroupItem>
			<ToggleGroupItem value="system" aria-label="System">
				<MonitorIcon aria-hidden="true" />
			</ToggleGroupItem>
		</ToggleGroup>
	);
}
