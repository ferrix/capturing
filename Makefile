COVER_PKGS := ./pkg/bmc/... ./internal/controller/...
COVER_PKG := github.com/ferrix/capturing/pkg/bmc,github.com/ferrix/capturing/pkg/bmc/fake,github.com/ferrix/capturing/internal/controller
COVER_MIN := 90

.PHONY: test cover generate fmt vet

test: generate
	go test ./... -count=1
	$(MAKE) cover

cover:
	go test $(COVER_PKGS) -count=1 -covermode=atomic -coverpkg=$(COVER_PKG) -coverprofile=cover.out
	@pct=$$(go tool cover -func=cover.out | awk '/^total:/ {gsub(/%/,"",$$3); print $$3}'); \
	echo "coverage $$pct%"; \
	awk -v p=$$pct -v m=$(COVER_MIN) 'BEGIN { if ((p+0) < m) { printf "coverage %s%% < %s%%\n", p, m; exit 1 } }'

generate:
	go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.23.0 \
		object:headerFile=hack/boilerplate.go.txt paths=./api/...
	go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.23.0 \
		crd:crdVersions=v1 \
		rbac:roleName=capturing-manager-role \
		paths=./... \
		output:crd:artifacts:config=config/crd/bases \
		output:rbac:artifacts:config=config/rbac

fmt:
	go fmt ./...

vet:
	go vet ./...
