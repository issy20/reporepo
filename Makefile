BINARY  := reporepo
PKG     := ./...
DIST    := dist

# クロスコンパイル対象（CGO 不要 → 単一バイナリ配布）
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build test lint fmt vet tidy run smoke cross release clean

build: ## バイナリをビルド
	go build -o $(BINARY) .

test: ## 全テストを実行
	go test $(PKG)

vet: ## go vet
	go vet $(PKG)

fmt: ## gofmt でフォーマット
	gofmt -w .

lint: vet ## go vet + gofmt 差分チェック
	@test -z "$$(gofmt -l .)" || (echo "gofmt 未整形のファイルがあります:"; gofmt -l .; exit 1)

tidy: ## 依存を整理
	go mod tidy

run: ## ローカル実行
	go run .

smoke: ## API通信を伴わないCLIスモークテスト
	go run . --help
	go run . version
	go run . where

cross: ## 全プラットフォーム向けにビルド
	@mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		ext=""; if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -o $(DIST)/$(BINARY)_$${os}_$${arch}$$ext . ; \
	done

release: ## GoReleaser でリリース（スナップショット検証は goreleaser release --snapshot --clean）
	goreleaser release --clean

clean: ## 生成物を削除
	rm -rf $(BINARY) $(DIST)
