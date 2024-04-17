include .bingo/Variables.mk

PROTO_VERSIONS ?= $(shell ls ./prompb/write)
FILES_TO_FMT ?= $(shell find . -path ./vendor -prune -o -name '*.go' -print)

.PHONY: help
help: ## Display this help and any documented user-facing targets. Other undocumented targets may be present in the Makefile.
help:
	@awk 'BEGIN {FS = ": ##"; printf "Usage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_\.\-\/%]+: ##/ { printf "  %-45s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: test
test: ## Test
	go test ./...

.PHONY: format
format: ## Formats Go code.
format: $(GOIMPORTS)
	@echo ">> formatting code"
	@$(GOIMPORTS) -w $(FILES_TO_FMT)

.PHONY: proto
proto: ## Regenerate Go from proto
proto: $(BUF) $(PROTOC_GEN_GO_VTPROTO)
	@for version in $(PROTO_VERSIONS); do \
    	echo ">> regenerating $$version" ; \
    	$(BUF) generate --template prompb/write/$$version/buf.gen.yaml --path prompb/write/$$version proto ; \
	done

.PHONY: lint
lint: ## Runs various static analysis against our code.
lint: $(COPYRIGHT) $(BUF) format
	$(call require_clean_work_tree,"detected not clean main before running lint")
	@echo ">> linting proto"
	@$(BUF) lint ./proto
	@echo ">> ensuring Copyright headers"
	@$(COPYRIGHT) $(shell go list -f "{{.Dir}}" ./... | xargs -i find "{}" -name "*.go")
	$(call require_clean_work_tree,"detected white noise or/and files without copyright; run 'make lint' file and commit changes.")
