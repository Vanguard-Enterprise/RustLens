import { headers } from "next/headers";
import {
  Activity,
  ArrowRight,
  Bot,
  Database,
  Disc3,
  ServerCog,
  ShieldCheck,
  Siren,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { fetchHealth, type HealthResponse } from "@/src/lib/health";

const platformStats = [
  {
    label: "Shards under coordination",
    value: "4 target",
    detail: "Backend lease orchestration and worker heartbeats stay visible in one place.",
    icon: ServerCog,
  },
  {
    label: "Linked systems",
    value: "3 services",
    detail: "Go backend, Next frontend, and Discord bot run as one local stack.",
    icon: Disc3,
  },
  {
    label: "Setup persistence",
    value: "Postgres",
    detail: "Guild setup state, log channel settings, and wipe reacts survive restarts.",
    icon: Database,
  },
];

const workflowCards = [
  {
    title: "Guild setup",
    description: "Bring role selection, logs channel wiring, and one-time setup state into a calmer admin surface.",
  },
  {
    title: "Wipe react operations",
    description: "Expose active wipe reacts, participation gaps, and schedule timing without jumping between Discord commands.",
  },
  {
    title: "Identity linking",
    description: "Make Steam, Discord, and BattleMetrics links searchable with clear trust levels for soft and hard matches.",
  },
];

function getOrigin() {
  const requestHeaders = headers();
  const host = requestHeaders.get("x-forwarded-host") ?? requestHeaders.get("host");
  const protocol = requestHeaders.get("x-forwarded-proto") ?? "http";

  if (!host) {
    return "http://localhost";
  }

  return `${protocol}://${host}`;
}

function getHealthTone(health: HealthResponse | null) {
  if (!health) {
    return { label: "Backend unreachable", variant: "destructive" as const };
  }

  if (health.db === "ok" && health.redis === "ok") {
    return { label: "All systems nominal", variant: "default" as const };
  }

  return { label: "Partial degradation", variant: "secondary" as const };
}

export default async function HomePage() {
  const health = await fetchHealth(getOrigin());
  const healthTone = getHealthTone(health);

  return (
    <main className="relative overflow-hidden">
      <div className="absolute inset-x-0 top-0 -z-10 h-[34rem] bg-gradient-to-b from-muted via-background to-background" />
      <div className="absolute left-[-8rem] top-12 -z-10 size-64 rounded-full bg-muted/80 blur-3xl" />
      <div className="absolute right-[-6rem] top-20 -z-10 size-72 rounded-full bg-primary/10 blur-3xl" />
      <div className="mx-auto flex min-h-screen w-full max-w-7xl flex-col gap-10 px-6 py-8 lg:px-10 lg:py-10">
        <section className="grid gap-6 lg:grid-cols-[1.45fr_0.85fr]">
          <div className="flex flex-col gap-6 py-4">
            <div className="flex flex-col gap-4">
              <Badge variant="secondary">Frontend foundation</Badge>
              <div className="flex max-w-3xl flex-col gap-4">
                <h1 className="max-w-4xl text-4xl font-medium tracking-tight text-balance md:text-6xl">
                  Rust server operations need more than slash commands.
                </h1>
                <p className="max-w-2xl text-base leading-7 text-muted-foreground md:text-lg">
                  This first frontend pass establishes a control-plane shell for shard visibility, guild setup,
                  wipe react operations, and identity linking. It is now built on `shadcn` primitives instead of
                  one-off markup.
                </p>
              </div>
              <div className="flex flex-wrap items-center gap-3">
                <Button>
                  <Activity data-icon="inline-start" />
                  Review system status
                  <ArrowRight data-icon="inline-end" />
                </Button>
                <Button variant="outline">
                  <Bot data-icon="inline-start" />
                  Map bot workflows
                </Button>
              </div>
            </div>
            <div>
              <div className="grid gap-3 md:grid-cols-3">
                {platformStats.map(({ detail, icon: Icon, label, value }) => (
                  <Card key={label} size="sm" className="bg-card/80 backdrop-blur-sm">
                    <CardHeader>
                      <CardTitle className="flex items-center gap-2 text-sm">
                        <Icon className="size-4" />
                        {label}
                      </CardTitle>
                      <CardDescription>{detail}</CardDescription>
                    </CardHeader>
                    <CardFooter>
                      <span className="text-sm font-medium">{value}</span>
                    </CardFooter>
                  </Card>
                ))}
              </div>
            </div>
          </div>

          <Card className="bg-card/90 backdrop-blur-sm">
            <CardHeader>
              <CardTitle>Live backend state</CardTitle>
              <CardDescription>
                The page checks the Go API through the same `/api` route the browser uses.
              </CardDescription>
              <CardAction>
                <Badge variant={healthTone.variant}>{healthTone.label}</Badge>
              </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <div className="grid gap-3 sm:grid-cols-3">
                <div className="flex flex-col gap-1 rounded-lg bg-muted p-3">
                  <span className="text-xs uppercase tracking-[0.2em] text-muted-foreground">API</span>
                  <span className="text-sm font-medium">{health?.status ?? "unreachable"}</span>
                </div>
                <div className="flex flex-col gap-1 rounded-lg bg-muted p-3">
                  <span className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Postgres</span>
                  <span className="text-sm font-medium">{health?.db ?? "unknown"}</span>
                </div>
                <div className="flex flex-col gap-1 rounded-lg bg-muted p-3">
                  <span className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Redis</span>
                  <span className="text-sm font-medium">{health?.redis ?? "unknown"}</span>
                </div>
              </div>
              <Separator />
              <pre className="overflow-x-auto rounded-lg bg-muted p-4 text-xs leading-6 text-muted-foreground">
                {JSON.stringify(health ?? { status: "unreachable" }, null, 2)}
              </pre>
            </CardContent>
          </Card>
        </section>

        <section className="grid gap-4 lg:grid-cols-[0.9fr_1.1fr]">
          <Card className="bg-primary text-primary-foreground ring-0">
            <CardHeader>
              <Badge variant="outline">What this frontend is for</Badge>
              <CardTitle>A durable operator surface for the RustLens stack</CardTitle>
              <p className="text-sm leading-6 text-primary-foreground/80">
                The goal is not a decorative landing page. It is to move guild administration and wipe-react
                workflows into an interface that reduces Discord-only operational load.
              </p>
            </CardHeader>
            <CardContent>
              <div className="grid gap-3">
                <div className="flex items-start gap-3 rounded-lg bg-primary-foreground/10 p-4">
                  <ShieldCheck className="mt-0.5 size-4 shrink-0" />
                  <p className="text-sm leading-6">
                    Bot-only setup actions can stay protected while the frontend handles navigation, visibility,
                    and auditability.
                  </p>
                </div>
                <div className="flex items-start gap-3 rounded-lg bg-primary-foreground/10 p-4">
                  <Siren className="mt-0.5 size-4 shrink-0" />
                  <p className="text-sm leading-6">
                    Schedule drift, missing reactants, and logs-channel configuration should become obvious before
                    they become support issues.
                  </p>
                </div>
              </div>
            </CardContent>
          </Card>

          <div className="grid gap-4 md:grid-cols-3">
            {workflowCards.map(({ description, title }) => (
              <Card key={title}>
                <CardHeader>
                  <CardTitle>{title}</CardTitle>
                  <CardDescription>{description}</CardDescription>
                </CardHeader>
                <CardFooter>
                  <span className="text-sm text-muted-foreground">Planned for the next frontend slices.</span>
                </CardFooter>
              </Card>
            ))}
          </div>
        </section>
      </div>
    </main>
  );
}
