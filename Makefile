SHELL := /bin/bash

# 目标执行的可执行文件名
ifeq ($(OS),Windows_NT)
	EXE := EricChatViewer.exe
else
	EXE := EricChatViewer
endif

# 是否有 go.mod（Go modules 模式）
ifeq ("$(wildcard go.mod)","go.mod")
	USE_MOD := 1
else
	USE_MOD := 0
endif

.PHONY: all build run clean deps test

# 默认目标
all: build

# 构建根目录的主包
build:
	@echo "==> Building $(EXE) ..."
	@if [ $(USE_MOD) -eq 1 ]; then \
		echo "Using Go modules..."; \
		go mod download; \
	else \
		echo "Using GOPATH dependencies..."; \
		go get -d ./...; \
	fi
	@go build -o $(EXE) .

# 运行已构建的程序（若需要直接启动，可取消注释）
# run: build
#	@echo "==> Running $(EXE) ..."
#	@./$(EXE) &

# 下载依赖（模块模式下执行 go mod download，非模块模式执行 go get -d）
deps:
ifeq ($(USE_MOD),1)
	@echo "==> Downloading Go modules..."
	@go mod download
else
	@echo "==> Downloading GOPATH dependencies..."
	@go get -d ./...
endif

# 运行测试（若有测试用例）
test:
	@echo "==> Running tests..."
	@go test ./...

# 清理构建产物
clean:
	@echo "==> Cleaning..."
	@rm -f $(EXE) 2>/dev/null || true

# 交叉编译（可选，用于在 Windows/Linux/macOS 之间切换）
.PHONY: cross-windows cross-linux

cross-windows:
	@echo "==> Cross-building Windows binary ..."
	@GOOS=windows GOARCH=amd64 go build -o EricChatViewer.exe .
