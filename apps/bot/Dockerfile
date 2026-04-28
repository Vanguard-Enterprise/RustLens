FROM node:25-alpine

WORKDIR /workspace
COPY . .

RUN apk add --no-cache fontconfig ttf-dejavu
RUN npm install -g pnpm@10.6.0
RUN pnpm install --frozen-lockfile=false
RUN pnpm --filter @rustlens/bot build

CMD ["pnpm", "--filter", "@rustlens/bot", "start"]
