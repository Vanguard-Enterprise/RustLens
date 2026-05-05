"use client";

import { CheckIcon, DownloadIcon } from "lucide-react";
import * as React from "react";

import { Button } from "@/components/ui/button";

type Props = {
	url: string;
	label: string;
};

export function ExportButton({ url, label }: Props) {
	const [confirmed, setConfirmed] = React.useState(false);
	return (
		<Button
			asChild
			size="sm"
			variant="outline"
			onClick={() => {
				setConfirmed(true);
				setTimeout(() => setConfirmed(false), 2500);
			}}
			title="Logged to audit"
		>
			<a href={url} download>
				{confirmed ? (
					<>
						<CheckIcon className="mr-2 size-4" />
						Logged to audit
					</>
				) : (
					<>
						<DownloadIcon className="mr-2 size-4" />
						{label}
					</>
				)}
			</a>
		</Button>
	);
}
