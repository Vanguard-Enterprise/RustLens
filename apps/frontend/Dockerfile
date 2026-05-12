FROM node:26-alpine

WORKDIR /workspace
COPY . .

RUN npm install -g pnpm@10.6.0
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @rustlens/frontend build

EXPOSE 3000
CMD ["pnpm", "--filter", "@rustlens/frontend", "start"]
