.PHONY: help build build-frontend build-backend build-all clean test dev-frontend dev-backend install

BINARY_NAME=proxyhub
VERSION?=dev
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

help: ## 显示帮助信息
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build-frontend: ## 构建前端
	@echo "🎨 构建前端..."
	cd web && npm install && npm run build
	@echo "✅ 前端构建完成"

build-backend: ## 构建后端
	@echo "🔧 构建后端..."
	go build $(LDFLAGS) -o dist/$(BINARY_NAME) ./cmd/server
	@echo "✅ 后端构建完成: dist/$(BINARY_NAME)"

build: build-frontend build-backend ## 完整构建（前端+后端）

build-all: build-frontend ## 构建所有平台
	@echo "🔧 构建多平台二进制..."
	@mkdir -p dist/{linux-amd64,linux-arm64,darwin-amd64,darwin-arm64,windows-amd64}
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/linux-amd64/$(BINARY_NAME) ./cmd/server
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/linux-arm64/$(BINARY_NAME) ./cmd/server
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/darwin-amd64/$(BINARY_NAME) ./cmd/server
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/darwin-arm64/$(BINARY_NAME) ./cmd/server
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/windows-amd64/$(BINARY_NAME).exe ./cmd/server
	@echo "✅ 多平台构建完成"

test: ## 运行所有测试
	go test ./...

test-v: ## 运行测试（详细输出）
	go test -v ./...

test-cover: ## 运行测试并生成覆盖率报告
	go test -cover ./...
	go test -coverprofile=.test/coverage.out ./...
	go tool cover -html=.test/coverage.out -o .test/coverage.html
	@echo "✅ 覆盖率报告: .test/coverage.html"

lint: ## 代码检查
	golangci-lint run

dev-frontend: ## 前端开发服务器
	cd web && npm run dev

dev-backend: ## 后端开发服务器
	go run ./cmd/server -config config.example.yaml

clean: ## 清理构建产物
	rm -rf dist/*
	rm -rf .test/data/*.db .test/logs/* .test/tmp/* .test/*.out .test/*.html
	cd web && rm -rf dist node_modules

deps: ## 安装依赖
	go mod download
	go mod tidy
	cd web && npm install

docker-build: ## 构建 Docker 镜像
	docker build -t $(BINARY_NAME):$(VERSION) .

docker-run: ## 运行 Docker 容器
	docker run -d -p 8080:8080 -v ./data:/data $(BINARY_NAME):$(VERSION)

.DEFAULT_GOAL := help
