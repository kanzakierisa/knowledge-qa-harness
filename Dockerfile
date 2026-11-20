# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/knowledge-qa . \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/seed_demo_v2 ./scripts/seed_demo_v2.go

FROM alpine:3.21
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai

COPY --from=builder /out/knowledge-qa /app/knowledge-qa
COPY --from=builder /out/seed_demo_v2 /app/seed_demo_v2
COPY --from=builder /src/corpus /app/corpus
COPY --from=builder /src/corpus_v2 /app/corpus_v2
COPY --from=builder /src/migrations /app/migrations
COPY --from=builder /src/scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN sed -i 's/\r$//' /usr/local/bin/docker-entrypoint.sh \
 && chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=6 \
  CMD wget -q --spider http://127.0.0.1:8080/api/setup/status || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
