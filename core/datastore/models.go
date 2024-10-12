package datastore

import (
	"gorm.io/gorm"
)

type Graph struct {
	gorm.Model
	Hash       string `gorm:"uniqueIndex"`
	SortedJSON string
}

func (Graph) TableName() string {
	return "graphs"
}