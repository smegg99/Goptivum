// datastore/crud.go
package datastore

import (
	"fmt"

	"smuggr.xyz/goptivum/common/models"

	"github.com/dgraph-io/badger/v3"
	"google.golang.org/protobuf/proto"
)

func setItem(key []byte, item proto.Message) error {
	data, err := proto.Marshal(item)
	if err != nil {
		return err
	}

	return DB.Update(func(txn *badger.Txn) error {
		return txn.Set(key, data)
	})
}

func getItem(key []byte, item proto.Message) error {
	return DB.View(func(txn *badger.Txn) error {
		entry, err := txn.Get(key)
		if err != nil {
			return err
		}

		val, err := entry.ValueCopy(nil)
		if err != nil {
			return err
		}

		return proto.Unmarshal(val, item)
	})
}

func deleteItem(key []byte) error {
	return DB.Update(func(txn *badger.Txn) error {
		err := txn.Delete(key)
		if err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		return nil
	})
}

func SetDivision(division *models.Division) error {
	key := []byte(fmt.Sprintf("division:%d", division.Index))
	return setItem(key, division)
}

func GetDivision(index int64) (*models.Division, error) {
	key := []byte(fmt.Sprintf("division:%d", index))
	division := &models.Division{}
	err := getItem(key, division)
	if err != nil {
		return nil, err
	}
	return division, nil
}

func DeleteDivision(index int64) error {
	key := []byte(fmt.Sprintf("division:%d", index))
	return deleteItem(key)
}

func SetTeacher(teacher *models.Teacher) error {
	key := []byte(fmt.Sprintf("teacher:%d", teacher.Index))
	return setItem(key, teacher)
}

func GetTeacher(index int64) (*models.Teacher, error) {
	key := []byte(fmt.Sprintf("teacher:%d", index))
	teacher := &models.Teacher{}
	err := getItem(key, teacher)
	if err != nil {
		return nil, err
	}
	return teacher, nil
}

func DeleteTeacher(index int64) error {
	key := []byte(fmt.Sprintf("teacher:%d", index))
	return deleteItem(key)
}

func SetRoom(room *models.Room) error {
	key := []byte(fmt.Sprintf("room:%d", room.Index))
	return setItem(key, room)
}

func GetRoom(index int64) (*models.Room, error) {
	key := []byte(fmt.Sprintf("room:%d", index))
	room := &models.Room{}
	err := getItem(key, room)
	if err != nil {
		return nil, err
	}
	return room, nil
}

func DeleteRoom(index int64) error {
	key := []byte(fmt.Sprintf("room:%d", index))
	return deleteItem(key)
}

func SetMetadata(metadata *models.Metadata) error {
	key := []byte("metadata")
	return setItem(key, metadata)
}

func GetMetadata() (*models.Metadata, error) {
	key := []byte("metadata")
	metadata := &models.Metadata{}
	err := getItem(key, metadata)
	if err != nil {
		return nil, err
	}
	return metadata, nil
}
