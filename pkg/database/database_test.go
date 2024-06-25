package database

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewDatabase(t *testing.T) {
	if err := NewDatabase(); err != nil {
		t.Error("Expected database to be initialized")
	}

	createQuery := `CREATE TABLE cities (ip TEXT, city TEXT)`
	GetConnection().MustExec(createQuery)

	insertQuery := `INSERT INTO cities (ip, city) VALUES (?, ?)`

	tx := GetConnection().MustBegin()
	tx.MustExec(insertQuery, "83.121.11.105", "New York")
	tx.MustExec(insertQuery, "76.71.94.89", "Los Angeles")
	tx.MustExec(insertQuery, "204.195.163.16", "Chicago")

	err := tx.Commit()
	assert.NoError(t, err)
}
