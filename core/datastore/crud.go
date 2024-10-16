package datastore

import (
	"fmt"

	"smuggr.xyz/optivum-bsf/common/models"

	"github.com/dgraph-io/badger/v3"
	"google.golang.org/protobuf/proto"
)

func setItem(db *badger.DB, key []byte, item proto.Message) error {
    data, err := proto.Marshal(item)
    if err != nil {
        return err
    }

    return db.Update(func(txn *badger.Txn) error {
        return txn.Set(key, data)
    })
}

func getItem(db *badger.DB, key []byte, item proto.Message) error {
    return db.View(func(txn *badger.Txn) error {
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

func deleteItem(db *badger.DB, key []byte) error {
    return db.Update(func(txn *badger.Txn) error {
        err := txn.Delete(key)
        if err != nil && err != badger.ErrKeyNotFound {
            return err
        }
        return nil
    })
}

func SetDivision(db *badger.DB, division *models.Division) error {
    key := []byte(fmt.Sprintf("division:%d", division.Index))
    return setItem(db, key, division)
}

func GetDivision(db *badger.DB, index uint32) (*models.Division, error) {
    key := []byte(fmt.Sprintf("division:%d", index))
    division := &models.Division{}
    err := getItem(db, key, division)
    if err != nil {
        return nil, err
    }
    return division, nil
}

func DeleteDivision(db *badger.DB, index uint32) error {
    key := []byte(fmt.Sprintf("division:%d", index))
    return deleteItem(db, key)
}

func SetTeacher(db *badger.DB, teacher *models.Teacher) error {
    key := []byte(fmt.Sprintf("teacher:%d", teacher.Index))
    return setItem(db, key, teacher)
}

func GetTeacher(db *badger.DB, index uint32) (*models.Teacher, error) {
    key := []byte(fmt.Sprintf("teacher:%d", index))
    teacher := &models.Teacher{}
    err := getItem(db, key, teacher)
    if err != nil {
        return nil, err
    }
    return teacher, nil
}

func DeleteTeacher(db *badger.DB, index uint32) error {
    key := []byte(fmt.Sprintf("teacher:%d", index))
    return deleteItem(db, key)
}

func SetRoom(db *badger.DB, room *models.Room) error {
    key := []byte(fmt.Sprintf("room:%d", room.Index))
    return setItem(db, key, room)
}

func GetRoom(db *badger.DB, index uint32) (*models.Room, error) {
    key := []byte(fmt.Sprintf("room:%d", index))
    room := &models.Room{}
    err := getItem(db, key, room)
    if err != nil {
        return nil, err
    }
    return room, nil
}

func DeleteRoom(db *badger.DB, index uint32) error {
    key := []byte(fmt.Sprintf("room:%d", index))
    return deleteItem(db, key)
}