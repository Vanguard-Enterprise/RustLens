"use client";

import { useMemo, useRef, useState } from "react";
import { IconBrandDiscordFilled } from "@tabler/icons-react";
import {
	AtSignIcon,
	ChevronLeftIcon,
	KeyRoundIcon,
} from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { AuthDivider } from "@/components/auth-divider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	InputGroup,
	InputGroupAddon,
	InputGroupInput,
} from "@/components/ui/input-group";
import { Input } from "@/components/ui/input";

type AuthPageProps = {
	error?: string;
	notice?: string;
	showDiscord?: boolean;
	csrfToken: string;
};

type FlowMode = "email" | "login" | "register";

function getErrorMessage(error?: string) {
	switch (error) {
		case "discord-config":
			return "Discord OAuth is not configured yet.";
		case "discord-state":
			return "The Discord sign-in state was invalid or expired.";
		case "discord-token":
		case "discord-profile":
		case "discord-auth":
			return "Discord sign-in failed. Try again.";
		case "register-email":
			return "Enter an email address to continue.";
		case "register-password":
		case "login-password":
		case "reset-password":
			return "Enter a password with at least 12 characters.";
		case "register-conflict":
			return "That email already has a password login. Sign in instead.";
		case "login-invalid":
			return "Invalid email or password.";
		case "reset-request":
			return "We couldn't create a password reset right now.";
		case "reset-invalid":
			return "That password reset link is invalid or expired.";
		case "verify-request":
			return "We couldn't create an email verification link right now.";
		case "verify-invalid":
			return "That email verification link is invalid or expired.";
		case "email-already-verified":
			return "Your email is already verified.";
		case "auth-rate-limit":
			return "Too many attempts. Wait a moment and try again.";
		case "csrf":
			return "Your form expired or came from the wrong origin. Try again.";
		default:
			return "";
	}
}

function getNoticeMessage(notice?: string) {
	switch (notice) {
		case "registered":
			return "Account created.";
		case "reset-requested":
			return "If the account exists, a password reset link is ready to send.";
		case "password-reset":
			return "Password updated. Sign in with your new password.";
		case "email-verify-requested":
			return "An email verification link is ready to send.";
		case "email-verified":
			return "Email verified.";
		default:
			return "";
	}
}

function getInitialMode(error?: string): FlowMode {
	switch (error) {
		case "login-password":
		case "login-invalid":
			return "login";
		case "register-password":
			return "register";
		default:
			return "email";
	}
}

export function AuthPage({
	error,
	notice,
	showDiscord,
	csrfToken,
}: AuthPageProps) {
	const errorMessage = getErrorMessage(error);
	const noticeMessage = getNoticeMessage(notice);
	const [email, setEmail] = useState("");
	const [mode, setMode] = useState<FlowMode>(getInitialMode(error));
	const [lookupError, setLookupError] = useState("");
	const emailInputRef = useRef<HTMLInputElement | null>(null);

	const bannerMessage = lookupError || errorMessage;
	const heading = useMemo(() => {
		if (mode === "login") {
			return "Enter your password";
		}
		if (mode === "register") {
			return "Create your password";
		}
		return "Login to RustLens";
	}, [mode]);

	const subheading = useMemo(() => {
		if (mode === "email") {
			return "Sign up with email";
		}
		if (mode === "login") {
			return "";
		}
		if (mode === "register") {
			return "Sign up with email";
		}
		return "";
	}, [mode]);

	const continueWithEmail = () => {
		const normalizedEmail = email.trim();
		if (!normalizedEmail) {
			setLookupError("Enter an email address to continue.");
			return;
		}

		setLookupError("");
		setEmail(normalizedEmail);
		setMode("login");
	};

	const startSignUp = () => {
		const normalizedEmail = email.trim();
		if (!normalizedEmail) {
			setLookupError("Enter your email to start signing up.");
			emailInputRef.current?.focus();
			return;
		}

		setLookupError("");
		setMode("register");
	};

	return (
		<div className="relative w-full overflow-hidden md:h-screen">
			<div className="relative mx-auto flex min-h-screen w-full max-w-sm flex-col px-6 py-8 md:px-8 md:py-10">
				<div className="flex justify-center">
					<Link
						aria-label="RustLens home"
						className="flex items-center gap-3 rounded-lg px-3 py-2.5"
						href="/"
					>
						<Image
							alt="RustLens"
							className="h-10 w-auto md:h-12"
							height={48}
							priority
							src="/RustLens.png"
							width={48}
						/>
						<span className="text-lg font-semibold tracking-tight md:text-xl">
							RustLens
						</span>
					</Link>
				</div>

				<div className="fade-in slide-in-from-bottom-4 flex flex-1 items-center w-full animate-in duration-600">
					<div className="w-full space-y-4">
						<div className="flex flex-col gap-2 text-center">
							<div className="flex justify-center">
								<Button asChild size="sm" variant="ghost">
									<Link href="/">
										<ChevronLeftIcon data-icon="inline-start" />
										Back to home
									</Link>
								</Button>
							</div>
							<h1 className="font-heading text-3xl tracking-tight">{heading}</h1>
							{subheading ? (
								mode === "email" ? (
									<Button
										className="mx-auto"
										onClick={startSignUp}
										size="sm"
										type="button"
										variant="link"
									>
										Sign up with email
									</Button>
								) : (
									<p className="text-sm leading-6 text-muted-foreground">{subheading}</p>
								)
							) : null}
						</div>

					{bannerMessage ? (
						<Badge className="justify-center" variant="destructive">
							{bannerMessage}
						</Badge>
					) : null}
					{noticeMessage ? (
						<Badge className="justify-center" variant="secondary">
							{noticeMessage}
						</Badge>
					) : null}

					{mode === "email" ? (
						<form
							className="flex flex-col gap-2"
							onSubmit={(event) => {
								event.preventDefault();
								continueWithEmail();
							}}
						>
							<InputGroup>
								<InputGroupInput
									autoComplete="email"
									ref={emailInputRef}
									name="email"
									onChange={(event) => {
										setEmail(event.target.value);
										if (lookupError) {
											setLookupError("");
										}
									}}
									placeholder="your.email@example.com"
									type="email"
									value={email}
								/>
								<InputGroupAddon align="inline-start">
									<AtSignIcon />
								</InputGroupAddon>
							</InputGroup>

							<Button className="w-full" size="sm" type="submit">
								Continue With Email
							</Button>
							<Button
								className="w-full"
								onClick={startSignUp}
								size="sm"
								type="button"
								variant="outline"
							>
								Create New Account
							</Button>
						</form>
					) : null}

					{mode === "login" ? (
						<form action="/auth/login" className="flex flex-col gap-2" method="post">
							<input name="csrfToken" type="hidden" value={csrfToken} />
							<input name="email" type="hidden" value={email} />
							<div className="flex items-center justify-between rounded-lg border border-border bg-background/70 px-3 py-2">
								<Button
									onClick={() => setMode("email")}
									size="sm"
									type="button"
									variant="ghost"
								>
									<ChevronLeftIcon data-icon="inline-start" />
									Change
								</Button>
								<div className="min-w-0">
									<p className="truncate text-sm font-medium">{email}</p>
								</div>
							</div>
							<Input
								autoComplete="current-password"
								name="password"
								placeholder="Password"
								type="password"
							/>
							<Button className="w-full" size="sm" type="submit">
								<KeyRoundIcon data-icon="inline-start" />
								Sign In
							</Button>
							<Link
								className="text-center text-sm text-muted-foreground underline underline-offset-4 hover:text-foreground"
								href="/forgot-password"
							>
								Forgot password?
							</Link>
						</form>
					) : null}

					{mode === "register" ? (
						<form action="/auth/register" className="flex flex-col gap-2" method="post">
							<input name="csrfToken" type="hidden" value={csrfToken} />
							<input name="email" type="hidden" value={email} />
							<input name="displayName" type="hidden" value="" />
							<div className="flex items-center justify-between rounded-lg border border-border bg-background/70 px-3 py-2">
								<Button
									onClick={() => setMode("email")}
									size="sm"
									type="button"
									variant="ghost"
								>
									<ChevronLeftIcon data-icon="inline-start" />
									Change
								</Button>
								<div className="min-w-0 text-right">
									<p className="truncate text-sm font-medium">{email}</p>
									<p className="text-xs text-muted-foreground">New account</p>
								</div>
							</div>
							<Input
								autoComplete="new-password"
								name="password"
								placeholder="Create password"
								type="password"
							/>
							<Button className="w-full" size="sm" type="submit">
								<KeyRoundIcon data-icon="inline-start" />
								Create Account
							</Button>
						</form>
					) : null}

					{showDiscord ? (
						<>
							<AuthDivider>OR CONTINUE WITH</AuthDivider>
							<Button asChild className="w-full" type="button" variant="outline">
								<Link href="/auth/discord">
									<IconBrandDiscordFilled data-icon="inline-start" />
									Discord
								</Link>
							</Button>
						</>
					) : null}

					</div>
				</div>

			</div>
		</div>
	);
}
