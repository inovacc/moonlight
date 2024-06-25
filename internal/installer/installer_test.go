//go:build windows

package installer

import (
	"context"
	"github.com/inovacc/moonlight/internal/config"
	"github.com/inovacc/moonlight/internal/util"
	"github.com/inovacc/moonlight/pkg/database"
	"log"
	"strings"
	"testing"
)

func TestNewInstaller(t *testing.T) {
	config.GetConfig.Db.DBPath = "."

	if err := database.NewDatabase(); err != nil {
		t.Error("Expected database to be initialized")
	}

	installer, err := NewInstaller(context.TODO(), database.GetConnection())
	if err != nil {
		t.Fatal(err)
	}

	if installer == nil {
		t.Fatal("Expected installer to be initialized")
	}

	list := []string{
		"go install github.com/spf13/cobra-cli@latest",
		"go install github.com/xo/xo@latest",
		"go install github.com/BurntSushi/toml/cmd/tomlv@latest",
		"go install github.com/dyammarcano/rpmbuild-cli@latest",
		"go install github.com/dyammarcano/rpmbuild-cli@latest",
		"go install golang.org/x/vuln/cmd/govulncheck@latest",
		"go install github.com/cavaliercoder/go-rpm/cmd/rpmdump@latest",
		"go install github.com/spf13/cobra-cli@latest",
		"go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest",
		"go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest",
		"go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28",
		"go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2",
		"go install github.com/oklog/ulid/v2/cmd/ulid@latest",
		"go install github.com/cosmtrek/air/@latest",
		"go install github.com/dgraph-io/badger/v4/badger@latest",
		"go install github.com/vugu/vgrun",
		"go install github.com/swaggo/swag/cmd/swag@latest",
		"go install github.com/ktr0731/evans@latest",
		"go install github.com/dgraph-io/badger/v4/badger@latest",
		"go install golang.org/x/vuln/cmd/govulncheck@latest",
		"go install honnef.co/go/tools/cmd/staticcheck@latest",
		"go install github.com/goreleaser/goreleaser@latest",
		"go install github.com/treeder/bump@latest",
		"go install github.com/treeder/bump",
		"go install github.com/treeder/bump@v1.3.0",
		"go install github.com/treeder/bump/cmd@v1.3.0",
		"go install github.com/treeder/bump/cmd@v1.3.0",
		"go install github.com/treeder/bump@v1.3.0",
		"go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest",
		"go install github.com/jmoiron/sqlx@latest",
		"go install github.com/golang/protobuf/protoc-gen-go@latest",
		"go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest",
		"go install github.com/k1LoW/tbls@latest",
		"go install github.com/dgraph-io/badger/v4/badger@latest",
		"go install github.com/klauspost/cpuid/v2/cmd/cpuid@latest",
		"go install github.com/klauspost/asmfmt/cmd/asmfmt@latest",
		"go install github.com/ServiceWeaver/weaver/cmd/weaver@latest",
		"go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest",
		"go install github.com/CycloneDX/cyclonedx-gomod@latest",
		"go install github.com/CycloneDX/cyclonedx-gomod@v1.0.0",
		"go install github.com/sigstore/cosign/v2/cmd/cosign@latest",
		"go install github.com/theupdateframework/go-tuf/cmd/tuf-client@latest",
		"go install github.com/daviddengcn/go-diff@latest",
		"go install go.starlark.net/cmd/starlark@latest",
		"go install github.com/goreleaser/goreleaser@latest",
		"go install github.com/dyammarcano/secure_message@latest",
		"go install github.com/BurntSushi/toml/cmd/tomlv@latest",
		"go install github.com/dyammarcano/secure_messagecmd/sm@latest",
		"go install github.com/dyammarcano/secure_message/cmd/sm@latest",
		"go install github.com/dyammarcano/secure_message/cmd/sm@v0.0.2",
		"go install github.com/CycloneDX/cyclonedx-gomod@latest",
		"go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest",
		"go install github.com/go-swagger/go-swagger/cmd/swagger@latest",
		"go install github.com/swaggo/swag/cmd/swag@latest",
		"go install github.com/rqlite/rqlite@latest",
		"go install github.com/rqlite/rqlite/cmd/rqlite@latest",
		"go install github.com/rqlite/rqlite/cmd/rqlited@latest",
		"go install github.com/rqlite/rqlite/cmd/rqlited@latest",
		"go install github.com/dyammarcano/version@add-cli",
		"go install github.com/dyammarcano/version",
		"go install github.com/dyammarcano/version@latest",
		"go install github.com/parvez3019/go-swagger3@latest",
		"go install github.com/ogen-go/ogen/cmd/ogen@latest",
		"go install go.etcd.io/bbolt/cmd/bbolt@latest",
		"go install github.com/golang-migrate/migrate@latest",
		"go install github.com/golang-migrate/migrate/cli@latest",
		"go install github.com/pressly/goose/v3/cmd/goose@latest",
		"go install github.com/pressly/goose/v3/cmd/goose@latest",
		"go install github.com/gomods/athens/tree/main/cmd/proxy@latest athens",
		"go install athens github.com/gomods/athens/tree/main/cmd/proxy@latest",
		"go install athens github.com/gomods/athens/tree/main/cmd/proxy@latest",
		"go install github.com/gomods/athens/tree/main/cmd/proxy@latest",
		"go install github.com/gomods/athens/cmd/proxy@latest",
		"go install github.com/gomods/athens/cmd/proxy@latest",
		"go install github.com/gomods/athens/cmd/proxy@latest",
		"go install github.com/spf13/cobra@latest",
		"go install github.com/spf13/cobra-cli@latest",
		"go install github.com/ktr0731/evans@latest",
		"go install github.com/go-delve/delve/cmd/dlv@latest",
		"go install github.com/go-delve/delve/cmd/dlv@latest",
		"go install github.com/google/gops@latest",
		"go install golang.org/x/tools/gopls@latest",
		"go install github.com/google/gops@latest",
		"go install github.com/segmentio/ksuid/cmd/ksuid",
		"go install github.com/segmentio/ksuid/cmd/ksuid@latest",
		"go install github.com/gin-gonic/gin@latest",
		"go install github.com/go-kratos/kratos/cmd/kratos/v2@latest",
		"go install github.com/rqlite/rqlite/cmd/rqlited@latest",
		"go install golang.org/x/tools/cmd/go-contrib-init@latest",
		"go install golang.org/x/review/git-codereview@latest",
		"go install github.com/google/ko@latest",
		"go install github.com/google/gops@v0.3.28",
		"go install github.com/google/gops@v0.3.27",
		"go install github.com/rqlite/rqlite/cmd/rqlited@latest",
		"go install go.etcd.io/bbolt/cmd/bbolt@v1.3.10",
		"go install go.etcd.io/bbolt/cmd/bbolt@latest",
		"go install go.etcd.io/bbolt/cmd/bbolt@v1.3.9",
	}

	var errList []string
	output := util.RemoveDuplicates(list)

	current := 0
	for _, item := range output {
		current++
		v := strings.Split(item, " ")
		log.Printf("task %d/%d, installing: %s", current, len(output), v[len(v)-1])
		if err = installer.Command(item); err != nil {
			errList = append(errList, err.Error())
		}
	}

	if len(errList) > 0 {
		t.Errorf("Expected no errors but got %v", errList)
	}
}
