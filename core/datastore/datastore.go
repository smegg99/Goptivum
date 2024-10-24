// datastore/datastore.go
package datastore

import (
	"fmt"
	"os"

	"github.com/dgraph-io/badger/v3"
	"github.com/dgraph-io/badger/v3/options"
)

var DB *badger.DB

func Initialize() error {
	fmt.Println("initializing datastore")

	opts := badger.DefaultOptions(os.Getenv("DB_FILE_PATH"))
    opts = opts.WithCompression(options.ZSTD)
	opts = opts.WithZSTDCompressionLevel(1)
	
    db, err := badger.Open(opts)
    if err != nil {
        return err
    }
	DB = db

	return nil
}

func Cleanup() {
	fmt.Println("cleaning datastore")
	if err := DB.Close(); err != nil {
		panic(err)
	}
}
