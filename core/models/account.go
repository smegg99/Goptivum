// core/datastore/company.go
package models

import (
	"database/sql/driver"

	"smegg.me/goptivum/core/auth"
)

type AccountRole uint

const (
	AccountRoleAdmin  AccountRole = 0
	AccountRoleMember AccountRole = 1
)

func (r AccountRole) String() string {
	switch r {
	case AccountRoleAdmin:
		return "ADMIN"
	case AccountRoleMember:
		return "MEMBER"
	default:
		return "UNKNOWN"
	}
}

func (r AccountRole) Value() (driver.Value, error) {
	return int64(r), nil
}

type Account struct {
	Login        string      `json:"login" validate:"required,min=1,max=255,excludesall= \t\n\r"`
	PasswordHash string      `json:"-" validate:"required,min=1"`
	Role         AccountRole `json:"role" validate:"-"`
}

func NewAccount(login, password string, role AccountRole) (*Account, error) {
	account := &Account{}

	account.SetRole(role)

	if err := account.SetLogin(login); err != nil {
		return nil, err
	}
	if err := account.SetPassword(password); err != nil {
		return nil, err
	}
	if err := account.Validate(); err != nil {
		return nil, err
	}
	return account, nil
}

func (c *Account) SetRole(newRole AccountRole) {
	c.Role = newRole
}

func (c *Account) SetPassword(password string) error {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	c.PasswordHash = hash
	return nil
}

func (c *Account) SetLogin(login string) error {
	if err := auth.ValidateLogin(login); err != nil {
		return err
	}
	c.Login = login
	return nil
}

func (c *Account) CheckPassword(password string) bool {
	return auth.ValidatePassword(password, c.PasswordHash)
}

func (c *Account) Validate() error {
	return Validate.Struct(c)
}
