# Android 中国象棋 AI 辅助系统

BIN      := bin/assistant
PIKAFISH := bin/pikafish
SRC      := third_party/Pikafish
# Apple Silicon 用 apple-silicon；Intel Mac 改成 x86-64；Linux 用 x86-64 或 armv8
ARCH     ?= apple-silicon

.PHONY: all build pikafish test test-short vet fmt clean testdata run analyze

all: build pikafish

## build: 编译主程序
build:
	@mkdir -p bin
	go build -o $(BIN) ./cmd/assistant
	@echo "已生成 $(BIN)"

## pikafish: 克隆并编译 Pikafish 引擎，连同 NNUE 权重一起放进 bin/
pikafish:
	@if [ ! -d $(SRC) ]; then \
		echo "克隆 Pikafish…"; \
		mkdir -p third_party; \
		git clone --depth 1 https://github.com/official-pikafish/Pikafish.git $(SRC); \
	fi
	@echo "编译 Pikafish（ARCH=$(ARCH)）…"
	$(MAKE) -C $(SRC)/src -j8 build ARCH=$(ARCH)
	@mkdir -p bin
	cp $(SRC)/src/pikafish $(PIKAFISH)
	cp $(SRC)/src/pikafish.nnue bin/pikafish.nnue
	@echo "已生成 $(PIKAFISH) 与 bin/pikafish.nnue"

## test: 运行全部测试
test:
	go test ./... -timeout 300s

## test-short: 跳过耗时的深度 perft
test-short:
	go test ./... -short -timeout 120s

## vet: 静态检查
vet:
	go vet ./...

## fmt: 格式化
fmt:
	gofmt -w ./cmd ./internal

## testdata: 生成演示用的画面序列
testdata: build
	./$(BIN) simulate

## run: 回放演示画面，不用手机也能看到完整链路
run: testdata
	./$(BIN) watch -frames testdata/game

## analyze: 从初始局面走两步并给出建议
analyze: build
	./$(BIN) analyze 炮二平五 马8进7

## clean: 清理构建产物
clean:
	rm -rf bin debug testdata/game

## help: 显示可用目标
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
