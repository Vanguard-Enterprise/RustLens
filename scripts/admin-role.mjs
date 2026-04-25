#!/usr/bin/env node
// Promote or demote an app_users role by Discord ID.
// Usage:
//   pnpm admin:promote <discord_id> [role]   (default role: internal_admin)
//   pnpm admin:demote  <discord_id> <role>
//
// Reads DATABASE_URL from the environment (same env the backend uses).

import pg from "pg";

const { Client } = pg;

const action = process.argv[2]; // "promote" | "demote"
const discordId = process.argv[3];
const role = process.argv[4] ?? "internal_admin";

if (!action || !["promote", "demote"].includes(action) || !discordId) {
  console.error("Usage: admin-role.mjs <promote|demote> <discord_id> [role]");
  process.exit(1);
}

const url = process.env.DATABASE_URL;
if (!url) {
  console.error("DATABASE_URL must be set");
  process.exit(1);
}

const client = new Client({ connectionString: url });
await client.connect();

try {
  const { rows } = await client.query(
    "SELECT id, roles FROM app_users WHERE discord_id = $1",
    [discordId],
  );
  if (rows.length === 0) {
    console.error(`No app_users row with discord_id=${discordId}. Sign in via Discord first.`);
    process.exit(2);
  }

  const { id, roles: current } = rows[0];
  const before = Array.isArray(current) ? current : JSON.parse(current);
  let next;
  if (action === "promote") {
    next = before.includes(role) ? before : [...before, role];
  } else {
    next = before.filter((r) => r !== role);
  }

  await client.query(
    "UPDATE app_users SET roles = $1::jsonb, updated_at = NOW() WHERE id = $2",
    [JSON.stringify(next), id],
  );

  console.log(`user=${id} discord_id=${discordId}`);
  console.log(`  before: ${JSON.stringify(before)}`);
  console.log(`  after:  ${JSON.stringify(next)}`);
} finally {
  await client.end();
}
