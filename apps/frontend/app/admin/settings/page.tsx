import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";

export default function AdminSettingsPage() {
	return (
		<Card>
			<CardHeader>
				<CardTitle>Admin settings</CardTitle>
				<CardDescription>
					This area will host internal controls and policy toggles in later
					iterations.
				</CardDescription>
			</CardHeader>
			<CardContent>
				<p className="text-muted-foreground text-sm">
					V1 focuses on access control, inventory, and audit visibility.
				</p>
			</CardContent>
		</Card>
	);
}
