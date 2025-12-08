// core/repositories/account.go
package repositories

import (
	"encoding/json"
	"fmt"

	"github.com/dgraph-io/badger/v4"
	"smegg.me/goptivum/core/models"
)

type AccountRepository struct {
	db *badger.DB
}

func GetAccountRepository(db *badger.DB) *AccountRepository {
	return &AccountRepository{db: db}
}

func (r *AccountRepository) GetAccountByLogin(login string) (*models.Account, error) {
	var account *models.Account

	err := r.db.View(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("account:login:%s", login))
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			account = &models.Account{}
			return json.Unmarshal(val, account)
		})
	})

	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("account not found")
	}

	return account, err
}

func (r *AccountRepository) CreateAccount(account *models.Account) error {
	return r.db.Update(func(txn *badger.Txn) error {
		data, err := json.Marshal(account)
		if err != nil {
			return err
		}

		keyLogin := []byte(fmt.Sprintf("account:login:%s", account.Login))
		return txn.Set(keyLogin, data)
	})
}
