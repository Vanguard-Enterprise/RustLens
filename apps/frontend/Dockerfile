FROM node:26-alpine

WORKDIR /workspace
COPY . .

RUN npm install -g pnpm@10.6.0
RUN pnpm install --frozen-lockfile

# Server Actions: the encryption key (BuildKit secret) gets baked into the
# build output so every replica decrypts the same payloads; DEPLOYMENT_VERSION
# (set as ENV so it survives into the runtime container) feeds next.config.ts'
# deploymentId for client/server skew detection.
ARG DEPLOYMENT_VERSION
ENV DEPLOYMENT_VERSION=${DEPLOYMENT_VERSION}

RUN --mount=type=secret,id=next_server_actions_encryption_key \
    NEXT_SERVER_ACTIONS_ENCRYPTION_KEY="$(cat /run/secrets/next_server_actions_encryption_key)" \
    pnpm --filter @rustlens/frontend build

EXPOSE 3000
CMD ["pnpm", "--filter", "@rustlens/frontend", "start"]
