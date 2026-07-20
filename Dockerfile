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

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags "-s -w" -o proxyhub ./cmd/server

# 阶段 3: 最终镜像
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app

COPY --from=backend-builder /app/proxyhub .
COPY config.example.yaml /app/config.yaml

# 数据库落在 /data，便于挂载持久化
RUN mkdir -p /data
ENV PROXYHUB_DATA=/data

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1

CMD ["./proxyhub", "-config", "/app/config.yaml"]
