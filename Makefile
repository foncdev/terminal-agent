BIN := terminal-agent
PKG := ./cmd/terminal-agent
DIST := dist

# 릴리스 버전. 태그를 붙였으면 그걸 쓰고, 아니면 커밋 해시.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# 순수 Go로 유지한다. CGO를 켜면 크로스 컴파일이 즉시 아파진다.
export CGO_ENABLED = 0

LDFLAGS := -s -w -X main.version=$(VERSION)

# 배포 대상. 하나만 고쳐도 dist 전체가 따라온다.
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64

.PHONY: build run test vet check clean dist checksums

build:
	go build -ldflags="$(LDFLAGS)" -o $(BIN) $(PKG)

run:
	go run $(PKG)

test:
	go test ./... -timeout 120s

vet:
	go vet ./...

check: vet test

# 전 플랫폼 바이너리를 한 번에 뽑아 tar.gz로 묶는다.
#
# 설치 스크립트가 이 이름 규칙을 그대로 쓴다:
#   terminal-agent_<os>_<arch>.tar.gz
# 규칙을 바꾸면 install.sh도 같이 고쳐야 한다.
dist: clean
	@mkdir -p $(DIST)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		out=$(BIN); \
		[ "$$os" = "windows" ] && out=$(BIN).exe; \
		echo "  $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -ldflags="$(LDFLAGS)" -o $(DIST)/$$out $(PKG) || exit 1; \
		tar -czf $(DIST)/$(BIN)_$${os}_$${arch}.tar.gz -C $(DIST) $$out README.md 2>/dev/null \
			|| tar -czf $(DIST)/$(BIN)_$${os}_$${arch}.tar.gz -C $(DIST) $$out; \
		rm -f $(DIST)/$$out; \
	done
	@$(MAKE) --no-print-directory checksums
	@echo
	@ls -lh $(DIST)

# 받는 쪽이 무결성을 확인할 수 있게 한다.
checksums:
	@cd $(DIST) && shasum -a 256 *.tar.gz > checksums.txt 2>/dev/null \
		|| sha256sum *.tar.gz > checksums.txt

clean:
	@rm -rf $(DIST) $(BIN)
