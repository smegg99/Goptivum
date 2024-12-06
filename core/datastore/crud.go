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

func SetEntity[T proto.Message](prefix string, index int64, entity T) error {
	key := []byte(fmt.Sprintf("%s:%d", prefix, index))
	return setItem(key, entity)
}

func GetEntity[T proto.Message](prefix string, index int64, newEntity func() T) (T, error) {
	key := []byte(fmt.Sprintf("%s:%d", prefix, index))
	entity := newEntity()
	err := getItem(key, entity)
	if err != nil {
		var zero T
		return zero, err
	}
	return entity, nil
}

func DeleteEntity(prefix string, index int64) error {
	key := []byte(fmt.Sprintf("%s:%d", prefix, index))
	return deleteItem(key)
}

func SetFixedItem[T proto.Message](key []byte, item T) error {
	return setItem(key, item)
}

func GetFixedItem[T proto.Message](key []byte, newItem func() T) (T, error) {
	item := newItem()
	err := getItem(key, item)
	if err != nil {
		var zero T
		return zero, err
	}
	return item, nil
}

func DeleteFixedItem(key []byte) error {
	return deleteItem(key)
}

func SetDivision(division *models.Division) error {
	return SetEntity("division", division.Index, division)
}

func GetDivision(index int64) (*models.Division, error) {
	return GetEntity("division", index, func() *models.Division { return &models.Division{} })
}

func DeleteDivision(index int64) error {
	return DeleteEntity("division", index)
}

func SetTeacher(teacher *models.Teacher) error {
	return SetEntity("teacher", teacher.Index, teacher)
}

func GetTeacher(index int64) (*models.Teacher, error) {
	return GetEntity("teacher", index, func() *models.Teacher { return &models.Teacher{} })
}

func DeleteTeacher(index int64) error {
	return DeleteEntity("teacher", index)
}

func SetRoom(room *models.Room) error {
	return SetEntity("room", room.Index, room)
}

func GetRoom(index int64) (*models.Room, error) {
	return GetEntity("room", index, func() *models.Room { return &models.Room{} })
}

func DeleteRoom(index int64) error {
	return DeleteEntity("room", index)
}

func SetDivisionsMeta(metadata *models.Metadata) error {
	key := []byte("divisions_meta")
	return SetFixedItem(key, metadata)
}

func GetDivisionsMeta() (*models.Metadata, error) {
	key := []byte("divisions_meta")
	return GetFixedItem(key, func() *models.Metadata { return &models.Metadata{} })
}

func DeleteDivisionsMeta() error {
	key := []byte("divisions_meta")
	return DeleteFixedItem(key)
}

func SetTeachersMeta(metadata *models.Metadata) error {
	key := []byte("teachers_meta")
	return SetFixedItem(key, metadata)
}

func GetTeachersMeta() (*models.Metadata, error) {
	key := []byte("teachers_meta")
	return GetFixedItem(key, func() *models.Metadata { return &models.Metadata{} })
}

func DeleteTeachersMeta() error {
	key := []byte("teachers_meta")
	return DeleteFixedItem(key)
}

func SetRoomsMeta(metadata *models.Metadata) error {
	key := []byte("rooms_meta")
	return SetFixedItem(key, metadata)
}

func GetRoomsMeta() (*models.Metadata, error) {
	key := []byte("rooms_meta")
	return GetFixedItem(key, func() *models.Metadata { return &models.Metadata{} })
}

func DeleteRoomsMeta() error {
	key := []byte("rooms_meta")
	return DeleteFixedItem(key)
}

func SetTeachersOnDutyWeek(teachersOnDuty *models.TeachersOnDutyWeek) error {
	key := []byte("teachers_on_duty_week")
	return SetFixedItem(key, teachersOnDuty)
}

func GetTeachersOnDutyWeek() (*models.TeachersOnDutyWeek, error) {
	key := []byte("teachers_on_duty_week")
	return GetFixedItem(key, func() *models.TeachersOnDutyWeek { return &models.TeachersOnDutyWeek{} })
}

func DeleteTeachersOnDutyWeek() error {
	key := []byte("teachers_on_duty_week")
	return DeleteFixedItem(key)
}

func SetPractices(practices *models.Practices) error {
	key := []byte("practices")
	return SetFixedItem(key, practices)
}

func GetPractices() (*models.Practices, error) {
	key := []byte("practices")
	return GetFixedItem(key, func() *models.Practices { return &models.Practices{} })
}

func DeletePractices() error {
	key := []byte("practices")
	return DeleteFixedItem(key)
}