.PHONY: build install clean test run deps fmt vet gate

BINARY_NAME=hydra
INSTALL_PATH=$(HOME)/.local/bin
VERSION ?= dev
COMMIT ?=
BUILT_AT ?=

LDFLAGS=-X github.com/mssantosdev/hydra/internal/cmd.version=$(VERSION)
ifneq ($(strip $(COMMIT)),)
LDFLAGS += -X github.com/mssantosdev/hydra/internal/cmd.commit=$(COMMIT)
endif
ifneq ($(strip $(BUILT_AT)),)
LDFLAGS += -X github.com/mssantosdev/hydra/internal/cmd.builtAt=$(BUILT_AT)
endif

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

install: build
	mkdir -p $(INSTALL_PATH)
	cp $(BINARY_NAME) $(INSTALL_PATH)/
	@echo "Installed to $(INSTALL_PATH)/$(BINARY_NAME)"
	@echo "Make sure $(INSTALL_PATH) is in your PATH"

clean:
	rm -f $(BINARY_NAME)
	go clean

test:
	go test ./...

run: build
	./$(BINARY_NAME)

deps:
	go mod tidy
	go mod download

fmt:
	go fmt ./...

vet:
	go vet ./...

# gate is the pre-commit contract from AGENTS.md: formatting, vet, tests.
gate:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet ./...
	go test ./...
