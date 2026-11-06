# 茶与月色 —— 构建脚本（Linux / macOS；Windows 用 build.ps1）
# 依赖：Go 1.25+、Node 20+

BIN      := bin/musicd
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)

.PHONY: all build web server run dev-web dev-server test typecheck clean deploy-copy

all: build

web:
	npm --prefix web install --no-audit --no-fund
	npm --prefix web run build
	node scripts/copy-dist.mjs

server:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./server/cmd/musicd

build: web server

run: build
	./$(BIN)

dev-web:
	npm --prefix web run dev

dev-server:
	go run ./server/cmd/musicd

# 纯 UI 走查：不进后端，用内置 mock 数据
dev-mock:
	VITE_MOCK=1 npm --prefix web run dev

test:
	go test ./server/...

typecheck:
	npm --prefix web run typecheck

clean:
	rm -rf bin web/dist
	rm -rf server/internal/webui/dist/*
	@touch server/internal/webui/dist/.gitkeep
