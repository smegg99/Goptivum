// core/datastore/seeding.go
package datastore

import (
	"github.com/spf13/viper"
	"smegg.me/goptivum/common/logger"
	"smegg.me/goptivum/core/models"
	"smegg.me/goptivum/core/repositories"
)

func initializeAccount(login, password string, role models.AccountRole) {
	repo := repositories.GetAccountRepository(DB)

	existingAccount, _ := repo.GetAccountByLogin(login)
	if existingAccount == nil {
		account, err := models.NewAccount(login, password, role)
		if err != nil {
			logger.Errorf("failed to create account %s: %v", login, err)
		} else {
			if err := repo.CreateAccount(account); err != nil {
				logger.Errorf("failed to save account %s: %v", login, err)
			} else {
				logger.Infof("created account: %s", login)
			}
		}
	}
}

func initializeDefaultAccounts() {
	logger.Info("seeding default accounts")

	adminLogin := viper.GetString("ADMIN_ACCOUNT_LOGIN")
	adminPassword := viper.GetString("ADMIN_ACCOUNT_PASSWORD")

	initializeAccount(adminLogin, adminPassword, models.AccountRoleAdmin)

	defaultLogin := viper.GetString("DEFAULT_ACCOUNT_LOGIN")
	defaultPassword := viper.GetString("DEFAULT_ACCOUNT_PASSWORD")

	initializeAccount(defaultLogin, defaultPassword, models.AccountRoleMember)
}
