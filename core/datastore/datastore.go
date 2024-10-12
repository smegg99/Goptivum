package datastore

import (
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func GetDSN() string {
	return "host=" + os.Getenv("DB_HOST") +
		" port=" + os.Getenv("DB_PORT") +
		" user=" + os.Getenv("DB_USER") +
		" password=" + os.Getenv("DB_PASSWORD") +
		" dbname=" + os.Getenv("DB_NAME") +
		" sslmode=disable TimeZone=UTC"
}

func Initialize() error {
	db, err := gorm.Open(postgres.Open(GetDSN()))
	if err != nil {
		return err
	}

	DB = db

	if err := DB.AutoMigrate(&Graph{}); err != nil {
		return err
	}

	return nil
}

func Cleanup() error {
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}

	if sqlDB != nil {
		if err := sqlDB.Close(); err != nil {
			return err
		}
	}

	return nil
}
