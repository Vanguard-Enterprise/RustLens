import { fetchHealth } from "../src/lib/health";

export default async function HomePage() {
  const health = await fetchHealth();

  return (
    <main>
      <section>
        <h1>RustLens Platform</h1>
        <p>
          Frontend traffic is served by Next.js. Any request under <code>/api/*</code> is routed to the Go backend.
        </p>
        <h2>Backend Health</h2>
        <pre>{JSON.stringify(health ?? { status: "unreachable" }, null, 2)}</pre>
      </section>
    </main>
  );
}