# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS builder

ARG SERVICE

WORKDIR /src

COPY go.work go.work.sum ./
COPY pkg/proto/go.mod pkg/proto/go.sum ./pkg/proto/
COPY services/bank/go.mod services/bank/go.sum ./services/bank/
COPY services/exchange/go.mod services/exchange/go.sum ./services/exchange/
COPY services/gateway/go.mod services/gateway/go.sum ./services/gateway/
COPY services/notification/go.mod services/notification/go.sum ./services/notification/
COPY services/user/go.mod services/user/go.sum ./services/user/

WORKDIR /src/services/${SERVICE}
RUN --mount=type=cache,target=/go/pkg/mod go mod download

WORKDIR /src
COPY pkg/proto ./pkg/proto
COPY services/${SERVICE} ./services/${SERVICE}

WORKDIR /src/services/${SERVICE}
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /out/service ./cmd

FROM alpine:3.19
COPY --from=builder /out/service /service
ENTRYPOINT ["/service"]
