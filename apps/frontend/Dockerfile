FROM node:20-alpine

WORKDIR /workspace
COPY . .

RUN corepack enable
RUN corepack prepare pnpm@10.6.0 --activate
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @rustlens/frontend build

EXPOSE 3000
CMD ["pnpm", "--filter", "@rustlens/frontend", "start"]
