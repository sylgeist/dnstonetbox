BINARY  := dnstonetbox
OPENBSD_TARGETS := openbsd/amd64 openbsd/arm64

.PHONY: build test vet lint tidy cross dist clean

build:
	go build -o $(BINARY) .

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

# requires: go install honnef.co/go/tools/cmd/staticcheck@latest
lint: vet
	staticcheck ./...

tidy:
	go mod tidy

cross:
	$(foreach target,$(OPENBSD_TARGETS), \
		CGO_ENABLED=0 GOOS=$(word 1,$(subst /, ,$(target))) \
		GOARCH=$(word 2,$(subst /, ,$(target))) \
		go build -o $(BINARY)-$(subst /,-,$(target)) .;)

dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=openbsd GOARCH=amd64  go build -o dist/$(BINARY)-openbsd-amd64  .
	CGO_ENABLED=0 GOOS=openbsd GOARCH=arm64  go build -o dist/$(BINARY)-openbsd-arm64  .
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64  go build -o dist/$(BINARY)-linux-amd64    .

clean:
	rm -f $(BINARY) $(BINARY)-openbsd-*
	rm -rf dist/
