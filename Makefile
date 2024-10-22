GOMODS := $(abspath $(shell find . -name "go.mod" | grep -v .bingo | xargs dirname))

DOCKER_IMAGE=quay.io/bwplotka/sink # gcr.io/gpe-test-1/sink
DOCKER_TAG=latest
DOCKER_PUSH="no"

# --- deps ---
GOFUMPT = goimports
$(GOIMPORTS):
	@go install golang.org/x/tools/cmd/goimports@latest

GOFUMPT = gofumpt
$(GOFUMPT):
	@go install mvdan.cc/gofumpt@latest

BUF = buf
$(BUF):
	@go install github.com/bufbuild/buf/cmd/buf@v1.39.0

MDOX = mdox
$(MDOX):
	@go install github.com/bwplotka/mdox@latest
# ------

.PHONY: help
help: ## Display this help and any documented user-facing targets. Other undocumented targets may be present in the Makefile.
help:
	@awk 'BEGIN {FS = ": ##"; printf "Usage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_\.\-\/%]+: ##/ { printf "  %-45s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)


.PHONY: docker
docker:
	@export DOCKER_IMAGE=$(DOCKER_IMAGE) DOCKER_TAG=$(DOCKER_TAG) && bash scripts/build-docker.sh $(DOCKER_PUSH)

.PHONY: test
test:
	@for gomod in $(GOMODS); do \
		cd $$gomod && go test -v ./...; \
    done

GO_FILES = $(shell find . -path ./vendor -prune -o -name '*.go' -print)

.PHONY: format
format: $(GOFUMPT) $(GOIMPORTS) $(MDOX)
	@echo ">> formating imports"
	@$(GOIMPORTS) -w $(GO_FILES)
	@echo ">> gofumpt-ing the code; golangci-lint requires this"
	@$(GOFUMPT) -extra -w $(GO_FILES)
	@echo ">> format documentation"
	@$(MDOX) fmt --soft-wraps ./*.md

.PHONY: proto
proto: ## Regenerate Go from proto.
proto: $(BUF)
	@$(MAKE) -C api/remotewrite proto BUF=$(BUF)

