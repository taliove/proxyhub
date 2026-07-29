.PHONY: help build build-frontend build-backend build-all clean test test-all test-v test-cover vet lint-frontend test-frontend test-shell check dev-frontend dev-backend start stop restart status install geoip-update geoip-check hooks

BINARY_NAME=proxyhub

# Embedded GeoIP (DB-IP Lite country mmdb, CC BY 4.0). The binary data file is
# never committed; `make geoip-update` downloads and stages it here for go:embed.
GEOIP_MMDB=internal/geoip/dbip-country.mmdb
VERSION?=dev
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

help: ## 显示帮助信息
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build-frontend: ## 构建前端
	@echo "🎨 构建前端..."
	cd web && npm install && npm run build
	@echo "✅ 前端构建完成"

geoip-update: ## 下载/刷新内嵌 GeoIP 数据库(DB-IP Lite,CC BY 4.0)
	@echo "🌍 更新 GeoIP 数据库..."
	bash scripts/geoip/update.sh
	@echo "✅ GeoIP 数据库就位: $(GEOIP_MMDB)"

geoip-check: ## 校验内嵌 GeoIP 数据库是否就位(缺失即硬失败)
	@test -f $(GEOIP_MMDB) || { \
		echo "❌ GeoIP 数据库缺失: $(GEOIP_MMDB)"; \
		echo "   run 'make geoip-update' first"; \
		exit 1; \
	}

build-backend: geoip-check ## 构建后端
	@echo "🔧 构建后端..."
	go build $(LDFLAGS) -o dist/$(BINARY_NAME) ./cmd/server
	@echo "✅ 后端构建完成: dist/$(BINARY_NAME)"

build: build-frontend build-backend ## 完整构建（前端+后端）

build-all: geoip-check build-frontend ## 构建所有平台
	@echo "🔧 构建多平台二进制..."
	@mkdir -p dist/{linux-amd64,linux-arm64,darwin-amd64,darwin-arm64,windows-amd64}
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/linux-amd64/$(BINARY_NAME) ./cmd/server
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/linux-arm64/$(BINARY_NAME) ./cmd/server
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/darwin-amd64/$(BINARY_NAME) ./cmd/server
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/darwin-arm64/$(BINARY_NAME) ./cmd/server
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/windows-amd64/$(BINARY_NAME).exe ./cmd/server
	@echo "✅ 多平台构建完成"

test: geoip-check ## 运行所有测试
	go test ./...

test-all: geoip-check ## 运行全部测试(与 test 等价,保留作完整性审计入口)
	go test ./...

test-v: geoip-check ## 运行测试（详细输出）
	go test -v ./...

test-cover: geoip-check ## 运行测试并生成覆盖率报告
	go test -cover ./...
	go test -coverprofile=.test/coverage.out ./...
	go tool cover -html=.test/coverage.out -o .test/coverage.html
	@echo "✅ 覆盖率报告: .test/coverage.html"

vet: geoip-check ## go vet 静态检查
	@echo "🔍 go vet..."
	go vet ./...

lint-frontend: ## 前端 lint + 格式检查(ESLint warn 不阻塞;Prettier 不贴合即失败)+ 类型检查
	@echo "🔍 前端 lint..."
	cd web && npm run lint && npm run format:check && npm run type-check

test-frontend: ## 前端单元测试(vitest,纯逻辑)
	@echo "🧪 前端单元测试..."
	cd web && npm run test

test-shell: ## 运行安装/运维/GeoIP 脚本测试套件(scripts/*/test_*.sh)
	@echo "🧪 运行 shell 测试套件..."
	@for d in scripts/install scripts/geoip; do \
		for t in $$d/test_*.sh; do \
			[ -f "$$t" ] || continue; \
			bash "$$t" > /dev/null 2>&1 && echo "  ✅ $$t" || { echo "  ❌ $$t"; exit 1; }; \
		done; \
	done

check: vet test test-shell lint-frontend ## 签入前聚合检查(vet + Go 测试 + shell 套件 + 前端 lint)
	@echo "✅ 全部检查通过"

lint: ## 代码检查
	golangci-lint run

dev-frontend: ## 前端开发服务器
	cd web && npm run dev

dev-backend: ## 后端开发服务器
	go run ./cmd/server -config config.example.yaml

# 运行生命周期:pid 文件驱动,幂等。restart = 停旧启新,不存在"第二个实例绑端口失败"
PID_FILE=var/log/proxyhub.pid
LOG_FILE=var/log/proxyhub.log

start: ## 后台启动服务(已在运行则提示)
	@if [ -f $(PID_FILE) ] && kill -0 $$(cat $(PID_FILE)) 2>/dev/null; then \
		echo "⚠️  已在运行 (PID: $$(cat $(PID_FILE))),如需重启用 make restart"; \
	else \
		mkdir -p var/log; \
		rm -f $(PID_FILE); \
		./dist/$(BINARY_NAME) > $(LOG_FILE) 2>&1 & \
		echo $$! > $(PID_FILE); \
		sleep 2; \
		if kill -0 $$(cat $(PID_FILE)) 2>/dev/null; then \
			echo "✅ 已启动 (PID: $$(cat $(PID_FILE))),访问 http://localhost:8080,日志 $(LOG_FILE)"; \
		else \
			rm -f $(PID_FILE); \
			echo "❌ 启动失败,查看日志: cat $(LOG_FILE)"; exit 1; \
		fi \
	fi

stop: ## 停止服务(未在运行则空操作)
	@if [ -f $(PID_FILE) ] && kill -0 $$(cat $(PID_FILE)) 2>/dev/null; then \
		kill $$(cat $(PID_FILE)); \
		echo "✅ 已停止 (PID: $$(cat $(PID_FILE)))"; \
	else \
		echo "未在运行"; \
	fi
	@rm -f $(PID_FILE)

restart: stop start ## 重启服务(停旧启新,幂等)

status: ## 查看服务状态
	@if [ -f $(PID_FILE) ] && kill -0 $$(cat $(PID_FILE)) 2>/dev/null; then \
		echo "运行中 (PID: $$(cat $(PID_FILE)),http://localhost:8080)"; \
	else \
		echo "未在运行"; \
	fi

clean: ## 清理构建产物
	rm -rf dist/*
	rm -rf .test/data/*.db .test/logs/* .test/tmp/* .test/*.out .test/*.html
	cd web && rm -rf dist node_modules

hooks: ## 激活本地 git hooks(core.hooksPath=.githooks,幂等)
	@git config core.hooksPath .githooks
	@echo "✅ git hooks 已激活: .githooks/"

deps: hooks ## 安装依赖(含激活 git hooks)
	go mod download
	go mod tidy
	cd web && npm install

docker-build: ## 构建 Docker 镜像
	docker build -t $(BINARY_NAME):$(VERSION) .

docker-run: ## 运行 Docker 容器
	docker run -d -p 8080:8080 -v ./data:/data $(BINARY_NAME):$(VERSION)

dev-docker: ## Docker 本机开发环境(构建+首次初始化+起服务,幂等;:18081)
	bash scripts/dev/dev-up.sh

dev-docker-down: ## 停 Docker 本机开发环境
	docker compose -f docker-compose.dev.yml down

dev-docker-logs: ## 看 Docker 本机开发环境日志
	docker compose -f docker-compose.dev.yml logs -f

.DEFAULT_GOAL := help
