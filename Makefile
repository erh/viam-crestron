GO_BUILD_ENV :=
GO_BUILD_FLAGS :=
MODULE_BINARY := bin/crestron-module

ifeq ($(VIAM_TARGET_OS), windows)
	GO_BUILD_ENV += GOOS=windows GOARCH=amd64
	GO_BUILD_FLAGS := -tags no_cgo
	MODULE_BINARY = bin/crestron-module.exe
endif

$(MODULE_BINARY): Makefile go.mod crestron/*.go component/*.go cmd/module/*.go
	$(GO_BUILD_ENV) go build $(GO_BUILD_FLAGS) -o $(MODULE_BINARY) ./cmd/module

lint:
	gofmt -s -w .

update:
	go get go.viam.com/rdk@latest
	go mod tidy

test:
	go test ./...

module.tar.gz: meta.json $(MODULE_BINARY)
ifneq ($(VIAM_TARGET_OS), windows)
	# strip to a fresh file and rename in, so this works even when the current
	# binary is busy (the module running in place during a reload).
	strip -o $(MODULE_BINARY).tmp $(MODULE_BINARY)
	mv -f $(MODULE_BINARY).tmp $(MODULE_BINARY)
endif
	tar czf $@ meta.json $(MODULE_BINARY)

module: test module.tar.gz

all: test module.tar.gz

setup:
	go mod tidy
