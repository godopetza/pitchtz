package initializers

import (
	"fmt"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func ConnectToDB() error {
	dsn := DatabaseDSN()
	if dsn == "" {
		return fmt.Errorf("DB or DATABASE_URL env is required")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	DB = db
	return nil
}

func DatabaseDSN() string {
	if dsn := os.Getenv("DB"); dsn != "" {
		return dsn
	}
	return os.Getenv("DATABASE_URL")
}
