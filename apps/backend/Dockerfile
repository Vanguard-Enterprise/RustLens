FROM golang:1.24-alpine AS base

WORKDIR /workspace
COPY . .

WORKDIR /workspace/apps/backend
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /backend ./cmd/server

FROM gcr.io/distroless/static:nonroot
COPY --from=base /backend /backend
EXPOSE 8080
ENTRYPOINT ["/backend"]