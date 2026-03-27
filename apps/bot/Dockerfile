FROM node:20-alpine

WORKDIR /workspace
COPY . .

RUN corepack enable
RUN corepack prepare pnpm@10.6.0 --activate
RUN pnpm install --frozen-lockfile=false
RUN pnpm --filter @rustlens/bot build

CMD ["pnpm", "--filter", "@rustlens/bot", "start"]