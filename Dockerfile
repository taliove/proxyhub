# 阶段 1: 构建前端（Vite 输出到 cmd/server/web，供后端 go:embed）
FROM node:22-alpine AS frontend-builder

WORKDIR /app
COPY web/package*.json ./web/
RUN cd web && npm ci
COPY web/ ./web/
RUN cd web && npm run build

# 阶段 2: 构建后端（内嵌前端产物，静态单二进制）
FROM golang:1.26-alpine AS backend-builder

WORKDIR /app
RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# 覆盖为前端构建产物（含 index.html + assets）
COPY --from=frontend-builder /app/cmd/server/web ./cmd/server/web

ARG VERSION=dev
# Build timestamp derived from SOURCE_DATE_EPOCH (reproducible, same contract
# as scripts/release/package.sh); empty means no buildTime stamp (local builds).
ARG SOURCE_DATE_EPOCH=""
RUN BUILD_TIME=""; \
    if [ -n "$SOURCE_DATE_EPOCH" ]; then \
      BUILD_TIME="$(date -u -d "@$SOURCE_DATE_EPOCH" '+%Y-%m-%d_%H:%M:%S')"; \
    fi; \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o proxyhub ./cmd/server

# 阶段 3: 最终镜像
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app

COPY --from=backend-builder /app/proxyhub .
# 容器配置由 config.example.yaml 派生(单一事实源):容器内必须监听
# 0.0.0.0 才能对外服务,数据库落在 /data 卷以便持久化。
COPY config.example.yaml /tmp/config.example.yaml
RUN sed -e 's/host: "127.0.0.1"/host: "0.0.0.0"/' \
        -e 's|path: "var/data/data.db"|path: "/data/data.db"|' \
        /tmp/config.example.yaml > /app/config.yaml && rm /tmp/config.example.yaml \
    && grep -q 'host: "0.0.0.0"' /app/config.yaml \
    && grep -q 'path: "/data/data.db"' /app/config.yaml

RUN mkdir -p /data

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

CMD ["./proxyhub", "-config", "/app/config.yaml"]
