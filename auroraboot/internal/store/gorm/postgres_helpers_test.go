package gorm_test

import (
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
)

// postgresDSNEnv names a PostgreSQL server the suite may create databases on.
// The specs that need one are skipped when it is unset.
const postgresDSNEnv = "AURORABOOT_TEST_POSTGRES_DSN"

// newPostgresStore opens a store on a database of its own, created on the
// server postgresDSNEnv names and dropped when the spec ends. It skips the
// spec when no server is configured. Call it from a BeforeEach or an It.
func newPostgresStore() *gormstore.Store {
	dsn := os.Getenv(postgresDSNEnv)
	if dsn == "" {
		Skip(postgresDSNEnv + " is not set")
	}
	base, err := url.Parse(dsn)
	Expect(err).NotTo(HaveOccurred())
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		sqlDB, err := admin.DB()
		Expect(err).NotTo(HaveOccurred())
		Expect(sqlDB.Close()).To(Succeed())
	})

	name := "auroraboot_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	Expect(admin.Exec("CREATE DATABASE " + name).Error).To(Succeed())
	DeferCleanup(func() {
		Expect(admin.Exec("DROP DATABASE " + name + " WITH (FORCE)").Error).To(Succeed())
	})

	target := *base
	target.Path = "/" + name
	s, err := gormstore.New(target.String())
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(s.Close()).To(Succeed()) })
	return s
}
