BIN := bin/saringan

.PHONY: build test vet fmt demo clean

build:
	go build -o $(BIN) ./cmd/saringan

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

demo: build
	@mkdir -p /tmp/saringan-demo
	@cp -r examples/* /tmp/saringan-demo/
	@cd /tmp/saringan-demo && ../../$(CURDIR)/$(BIN) check && \
	 ../../$(CURDIR)/$(BIN) run --policy agent-safe --source customers-file --stats

clean:
	rm -rf bin
