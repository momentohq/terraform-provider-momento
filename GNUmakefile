default: testacc

# Run acceptance tests
.PHONY: testacc
testacc:
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m

.PHONY: lint
lint:
	golangci-lint cache clean && golangci-lint run -v --concurrency 1

.PHONY: docs
docs:
	go generate
