# ---- dev:熱重載 ----
FROM golang:1.27-alpine AS dev
WORKDIR /app
RUN go install github.com/air-verse/air@v1.67.4
COPY go.mod go.sum ./
RUN go mod download
CMD ["air", "-c", ".air.toml"]

# ---- build ----
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cli ./cmd/cli

# ---- prod ----
FROM alpine:3.22 AS prod
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
USER app
COPY --from=build /out/api /usr/local/bin/api
# 維運指令(建立管理員、清理 token),以 docker compose run --rm cli ... 執行
COPY --from=build /out/cli /usr/local/bin/cli
EXPOSE 8080
ENTRYPOINT ["api"]
