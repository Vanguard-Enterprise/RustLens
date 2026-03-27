# Bot Agent Instructions

This file provides bot-specific guidance. It supplements the root `AGENTS.md`.

## Stack

- **Runtime**: Node.js 20, TypeScript (strict, ES2022, NodeNext)
- **Framework**: discord.js v14.18.0
- **Testing**: Vitest
- **Package**: `@rustlens/bot`

## Architecture

The bot is a sharded worker runtime. Each shard runs as a child process (`shard-worker.ts`) managed by `ShardRuntime`. The orchestrator (`index.ts`) registers with the Go backend, polls shard assignments, and sends heartbeats.

```
src/
  index.ts              # Orchestrator: register, reconcile, heartbeat
  shard-worker.ts       # Discord.js Client per shard + interaction handling
  shard-runtime.ts      # Child process manager
  shards.ts             # Shard delta computation
  backend-client.ts     # HTTP client for backend shard API (JWT auth)
  config.ts             # Env config loader
  setup-command.ts      # /setup start command registration + modal flow
  builders/             # Custom v2 modal component builders
```

Current command flow:
- `/setup start` opens a v2 modal with checkbox groups for roles and channels to create.
- After modal submit, setup edits the deferred ephemeral reply into a text progress bar instead of leaving Discord's default "thinking" state visible.
- The setup modal includes an `All` option in both checkbox groups. The `Bot` role is not shown in the modal — it is always created automatically when any role is selected. Role setup includes a hoisted soft-blue `Bot` role between `Co Leader` and `member`, assigned to the RustLens bot plus other bot members (fetched via `guild.members.fetch()` with cache fallback).
- Builder-only channels (`#build`, `builder call`) are skipped automatically when the `Builder` role is not selected.
- Channel setup includes `#wipe log` at the bottom of `Important` when announcements or wipe react channels are requested, supports an `Invite` modal option that creates `#press for invite`, and creates an `afk` voice channel at the bottom of `Voice Channels` while setting the guild AFK channel and Discord's nearest supported AFK timeout of 15 minutes.
- `/admin reset_setup` clears persisted setup state and removes roles and channels created by setup.
- Modal custom id: `setup_start_modal`
- Field custom ids: `setup_roles`, `setup_channels`
- Commands are registered globally by shard `0` on startup.
- Setup state is persisted through backend bot-only API routes so setup can only run once per guild until reset. Persisted data now includes selected roles and selected channels.
- Channel setup only bootstraps from a fresh guild with exactly one existing channel, and `/setup start` must be run from that channel because setup renames and deletes it after creating the new layout.
- When channel setup creates `#join`, the bot also tries to set it as the guild system channel and unsuppress Discord join notifications so member join messages land there.
- `/admin reset_setup` sends a summary reply after deletion, silently catching reply failures when the command channel was deleted during reset.

## Custom V2 Modal Builders

Discord.js v14 does not have builder classes for v2 modal components. We provide our own at `src/builders/`:

| Builder | Component Type | Purpose |
|---|---|---|
| `ModalV2Builder` | — | Top-level modal, auto-wraps components in Label (type 18) |
| `TextInputBuilder` | 4 | Short or paragraph text input |
| `RadioGroupBuilder` | 21 | Single choice radio buttons |
| `CheckboxGroupBuilder` | 22 | Multiple choice checkboxes (`setRequired(false)` allows `min_values: 0`) |
| `StringSelectBuilder` | 3 | Dropdown select menu |

Usage:
```ts
import {
  ModalV2Builder, RadioGroupBuilder, TextInputBuilder, TextInputStyle,
} from "./builders/index.js";

const modal = new ModalV2Builder()
  .setTitle("Feedback")
  .setCustomId("feedback_modal")
  .addComponent("Category", new RadioGroupBuilder()
    .setCustomId("cat")
    .addOptions({ label: "Bug", value: "bug" }, { label: "Feature", value: "feature" }))
  .addComponent("Details", new TextInputBuilder()
    .setCustomId("details")
    .setStyle(TextInputStyle.Paragraph));

await interaction.showModal(modal.toJSON());
```

Key design decisions:
- **No `this.data` pattern** — individual typed fields for better type safety and clean optional-field omission.
- **Label auto-wrapping** — `ModalV2Builder.addComponent(label, builder)` handles wrapping; component builders produce clean single-purpose JSON.
- **No base class** — builders are independent; they share almost no behavior.
- V2 component raw API reference is at `.docs/discord-modal-v2-components.md`.

## Conventions

- Use `type` aliases, not `interface`
- Use `private readonly` for internal collections
- ESM imports with `.js` extensions
- Co-locate tests as `*.test.ts` next to source
- Import vitest explicitly: `import { describe, expect, it } from "vitest"`

## Commands

```bash
pnpm --filter @rustlens/bot test    # Run tests (vitest)
pnpm --filter @rustlens/bot lint    # Type-check (tsc --noEmit)
pnpm --filter @rustlens/bot build   # Compile to dist/
pnpm --filter @rustlens/bot dev     # Hot-reload dev server
```
