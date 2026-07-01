# Build the identity service binary from the Cargo workspace.
FROM rust:1-slim-bookworm AS build
WORKDIR /workspace
COPY . .
RUN cargo build --release --bin identity

# Minimal runtime image (glibc + CA certs for outbound TLS to Discord/Redis).
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /workspace/target/release/identity /usr/local/bin/identity
EXPOSE 8090
ENTRYPOINT ["identity"]
