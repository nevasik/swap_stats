############################
# 1) Build stage
############################
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates

# Сначала зависимости для кеша
COPY go.mod go.sum ./
RUN go mod download

# Код
COPY . .

# Параметры сборки (прокинуть из CI). Пока опустили этот момент
ARG VERSION=dev
ARG COMMIT=none
ARG DATE
ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH}

# Собираем бинарь из cmd/aggregator
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/swap-stats ./cmd/aggregator

############################
# 2) Certs & tzdata stage
############################
FROM alpine:3.20 AS certs
RUN apk add --no-cache ca-certificates tzdata && update-ca-certificates

############################
# 3) Runtime (distroless)
############################
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app

# Бинарь
COPY --from=build /out/swap-stats /app/swap-stats

# Корневые сертификаты и часовые пояса, чтобы не было бага в части разницы пояса
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=certs /usr/share/zoneinfo /usr/share/zoneinfo

# Значения по умолчанию (можно переопределить переменными окружения)
ENV CONFIG=/app/config.yaml

# Документируем порты (HTTP, Prometheus, pprof)
EXPOSE 8080 9091 6060

# Нерутовый пользователь из distroless(выше описано)
USER nonroot:nonroot

ENTRYPOINT ["/app/swap-stats"]
