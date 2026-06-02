SHELL := /bin/bash

APP_NAME := EricChatViewer
MACOS_DIR := macOS
DMG_NAME  := $(APP_NAME).dmg

# 目标执行的可执行文件名（本机构建用）
ifeq ($(OS),Windows_NT)
	EXE := $(APP_NAME).exe
else
	EXE := $(APP_NAME)
endif

# 是否有 go.mod（Go modules 模式）
ifeq ("$(wildcard go.mod)","go.mod")
	USE_MOD := 1
else
	USE_MOD := 0
endif

.PHONY: all build run clean deps test cross cross-windows cross-macos-amd64 cross-macos-arm64 universal package-macos dmg

# 默认目标
all: build

# ─── 本机构建 ────────────────────────────────────────────────────────────────

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

# ─── 交叉编译 ────────────────────────────────────────────────────────────────

# Windows amd64
cross-windows:
	@echo "==> Cross-building Windows binary ..."
	@GOOS=windows GOARCH=amd64 go build -o $(APP_NAME).exe .
	@echo "    => $(APP_NAME).exe"

# macOS Intel (amd64)
cross-macos-amd64:
	@echo "==> Cross-building macOS Intel (amd64) binary ..."
	@GOOS=darwin GOARCH=amd64 go build -o $(APP_NAME)_amd64 .
	@echo "    => $(APP_NAME)_amd64"

# macOS Apple Silicon (arm64)
cross-macos-arm64:
	@echo "==> Cross-building macOS Apple Silicon (arm64) binary ..."
	@GOOS=darwin GOARCH=arm64 go build -o $(APP_NAME)_arm64 .
	@echo "    => $(APP_NAME)_arm64"

# ─── Universal Binary（Intel + M芯片 合并）────────────────────────────────────

universal: cross-macos-amd64 cross-macos-arm64
	@echo "==> Creating Universal Binary (lipo) ..."
	@lipo -create -output $(APP_NAME) $(APP_NAME)_amd64 $(APP_NAME)_arm64
	@echo "    => $(APP_NAME) (Universal)"
	@lipo -info $(APP_NAME)

# ─── 打包 macOS 目录 ─────────────────────────────────────────────────────────

# 只将 Universal Binary + 配置文件 复制到 macOS/ 目录（不含 Windows exe）
package-macos: universal
	@echo "==> Packaging into $(MACOS_DIR)/ ..."
	@mkdir -p $(MACOS_DIR)
	@cp $(APP_NAME)         $(MACOS_DIR)/$(APP_NAME)
	@cp post_config.json    $(MACOS_DIR)/post_config.json 2>/dev/null || true
	@cp proxy.txt           $(MACOS_DIR)/proxy.txt        2>/dev/null || true
	@echo "    => $(MACOS_DIR)/ 目录内容："
	@ls -lh $(MACOS_DIR)/

# ─── 打包 DMG ────────────────────────────────────────────────────────────────

dmg: package-macos
	@echo "==> Creating DMG: $(DMG_NAME) ..."
	@rm -f $(DMG_NAME)
	@hdiutil create \
		-volname "$(APP_NAME)" \
		-srcfolder "$(MACOS_DIR)" \
		-ov \
		-format UDZO \
		-o $(DMG_NAME)
	@echo "    => $(DMG_NAME) 打包完成！"
	@ls -lh $(DMG_NAME)

# ─── 一键全流程 ──────────────────────────────────────────────────────────────

# macOS DMG + Windows exe 分别独立构建
cross: dmg cross-windows
	@echo "==> 全流程完成！"

# ─── 辅助目标 ────────────────────────────────────────────────────────────────

deps:
ifeq ($(USE_MOD),1)
	@echo "==> Downloading Go modules..."
	@go mod download
else
	@echo "==> Downloading GOPATH dependencies..."
	@go get -d ./...
endif

test:
	@echo "==> Running tests..."
	@go test ./...

clean:
	@echo "==> Cleaning..."
	@rm -f $(EXE) $(APP_NAME) $(APP_NAME)_amd64 $(APP_NAME)_arm64 $(APP_NAME).exe $(DMG_NAME)
	@rm -f $(MACOS_DIR)/$(APP_NAME)
	@echo "    => 清理完成"
