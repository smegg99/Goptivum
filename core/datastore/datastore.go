// core/datastore/datastore.go
package datastore

import (
	"smegg.me/goptivum/common/logger"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/spf13/viper"
)

var DB *badger.DB

func Initialize() error {
	logger.Info("initializing badger database")

	opts := badger.DefaultOptions(viper.GetString("DB_FILE_PATH"))
	opts = opts.WithCompression(options.ZSTD)
	opts = opts.WithZSTDCompressionLevel(1)

	db, err := badger.Open(opts)
	if err != nil {
		return err
	}
	DB = db

	initializeDefaultAccounts()

	return nil
}

func Close() error {
	logger.Info("cleaning datastore")
	return DB.Close()
}

func GetDB() *badger.DB {
	return DB
}
