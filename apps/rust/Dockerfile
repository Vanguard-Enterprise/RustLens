# Build a RustLens service binary from the Cargo workspace. Pass the SERVICE
# build-arg to select which binary to build/run:
#   identity | players | servers | guilds | coordinator
FROM rust:1-slim-bookworm AS build
ARG SERVICE=identity
WORKDIR /workspace
COPY . .
RUN cargo build --release --bin "$SERVICE"

# Minimal runtime image (glibc + CA certs for outbound TLS to Discord/Redis).
FROM debian:bookworm-slim
ARG SERVICE=identity
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
# Copy the selected binary to a fixed path so the entrypoint is service-agnostic.
COPY --from=build /workspace/target/release/${SERVICE} /usr/local/bin/service
# Informational only; the actual port comes from the PORT env var (8090-8094).
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/service"]
