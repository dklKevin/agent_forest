# Convenience targets. The golden render/art tests are ordinary `go test`s;
# `make golden` just regenerates their fixtures so contributors do not have to
# remember the -update flag. The regenerated diff is the art review.
.PHONY: build test golden vet

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

# Regenerate every golden file, then show what moved.
golden:
	go test ./internal/gallery ./internal/render -update
	@git status --short -- '*/testdata/*.golden' || true
