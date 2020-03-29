package auth

import (
	"database/sql"
	"fmt"
	"strings"
)

// UserStoreConfig UserStoreConfig
type UserStoreConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
	Salt   string `yaml:"salt"`
}

// UserStore UserStore
type UserStore struct {
	config UserStoreConfig
	db     *sql.DB
}

// User User
type User struct {
	ID    int
	Login *string
	EMail *string
	Name  string
}

// NewUserStore constructor
func NewUserStore(db *sql.DB, config UserStoreConfig) *UserStore {
	return &UserStore{
		config: config,
		db:     db,
	}
}

// GetUserByCredentials GetUserByCredentials
func (s *UserStore) GetUserByCredentials(username string, password string) (*User, error) {
	if username == "" || password == "" {
		return nil, nil
	}

	column := "login"
	if strings.Contains(username, "@") {
		column = "e_mail"
	}

	item := &User{}

	row := s.db.QueryRow(
		fmt.Sprintf(
			`
				SELECT id, login, e_mail, name
				FROM users
				WHERE NOT deleted AND %s = ? AND password = MD5(CONCAT(?, ?))
			`,
			column,
		),
		username, s.config.Salt, password,
	)

	err := row.Scan(&item.ID, &item.Login, &item.EMail, &item.Name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return item, nil
}
