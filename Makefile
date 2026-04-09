ifeq ($(OS),Windows_NT)
DOCTOR_CMD := powershell -ExecutionPolicy Bypass -File scripts/setup/doctor.ps1
DEV_CMD := powershell -NoProfile -ExecutionPolicy Bypass -File scripts/dev/start-dev.ps1
else
DOCTOR_CMD := bash scripts/setup/doctor.sh
DEV_CMD := pnpm run dev:all
endif

.PHONY: setup doctor infra-up infra-down dev dev-backend dev-frontend dev-bot lint test build

setup:
	pnpm install
	cd apps/backend && go mod tidy
	cd apps/backend && go install github.com/air-verse/air@latest

doctor:
	$(DOCTOR_CMD)

infra-up:
	docker compose -f infra/docker-compose.yml up -d

infra-down:
	docker compose -f infra/docker-compose.yml down

dev: infra-up
	$(DEV_CMD)

dev-backend:
	cd apps/backend && go run github.com/air-verse/air@latest -c .air.toml

dev-frontend:
	pnpm --filter @rustlens/frontend dev

dev-bot:
	pnpm --filter @rustlens/bot dev

lint:
	pnpm lint
	cd apps/backend && go vet ./...

test:
	pnpm test
	cd apps/backend && go test ./...

build:
	pnpm build
	cd apps/backend && go build ./cmd/server
