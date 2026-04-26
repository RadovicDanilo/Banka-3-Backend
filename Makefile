-include .env
export

GO_IMAGE := golang:1.25
GO_RUN   := docker run --rm -v $(PWD):/app -w /app $(GO_IMAGE)

ADMIN_EMAIL  ?= admin@banka.raf
CLIENT_EMAIL ?= petar@primer.raf

MODULES := pkg/proto services/bank services/exchange services/gateway services/notification services/user

.PHONY: all up down down-v proto schema seed nuke lint lint-l build build-l test test-l test-integration test-integration-l fmt fmt-l

all: proto up schema seed

up:
	docker compose up -d --build

down:
	docker compose down

down-v:
	docker compose down -v

proto:
	docker build -t banka-proto -f scripts/proto/Dockerfile .
	docker run --rm -v $(PWD):/workspace -u $$(id -u):$$(id -g) banka-proto \
		--proto_path=/workspace/proto \
		--go_out=/workspace/pkg/proto --go_opt=paths=source_relative \
		--go-grpc_out=/workspace/pkg/proto --go-grpc_opt=paths=source_relative \
		$$(cd proto && find . -name '*.proto' | sed 's|^\./||')

schema:
	docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) < scripts/db/schema.sql

seed:
	docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) \
		-v admin_email=$(ADMIN_EMAIL) -v client_email=$(CLIENT_EMAIL) \
		< scripts/db/seed.sql

nuke:
	docker compose exec -T postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB) -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"

lint:
	@for m in $(MODULES); do \
		echo ">>> lint $$m"; \
		docker run --rm -v $(PWD):/app -w /app/$$m golangci/golangci-lint:v2.4 golangci-lint run ./... || exit 1; \
	done

lint-l:
	@for m in $(MODULES); do \
		echo ">>> lint $$m"; \
		(cd $$m && golangci-lint run ./...) || exit 1; \
	done

build:
	@for m in $(MODULES); do \
		echo ">>> build $$m"; \
		$(GO_RUN) sh -c "cd $$m && go build ./..." || exit 1; \
	done

build-l:
	@for m in $(MODULES); do \
		echo ">>> build $$m"; \
		(cd $$m && go build ./...) || exit 1; \
	done

test:
	@for m in $(MODULES); do \
		echo ">>> test $$m"; \
		$(GO_RUN) sh -c "cd $$m && go test -race -count=1 -tags=integration ./..." || exit 1; \
	done

test-l:
	@for m in $(MODULES); do \
		echo ">>> test $$m"; \
		(cd $$m && go test -race -count=1 -tags=integration ./...) || exit 1; \
	done

fmt:
	$(GO_RUN) gofmt -l -w services/ pkg/

fmt-l:
	gofmt -l -w services/ pkg/
