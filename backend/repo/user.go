package repo

import (
	"github.com/shinyhawk/Quiz/entity"
	"gorm.io/gorm"
)

type UserStore struct {
	DB *gorm.DB
}

func NewUserStore(db *gorm.DB) *UserStore {
	return &UserStore{
		DB: db,
	}
}

func (db *UserStore) Get(id uint) *entity.User {
	var user *entity.User
	db.DB.Where("id = ?", id).First(&user)
	if user.Id == 0 {
		return nil
	}
	return user
}

func (db *UserStore) GetByGoogle(id string) *entity.User {
	var user *entity.User
	db.DB.Where("google_id = ?", id).First(&user)
	if user.Id == 0 {
		return nil
	}
	return user
}

func (db *UserStore) GetByDiscord(id string) *entity.User {
	var user *entity.User
	db.DB.Where("discord_id = ?", id).First(&user)
	if user.Id == 0 {
		return nil
	}
	return user
}

func (db *UserStore) GetByTelegram(id string) *entity.User {
	var user *entity.User
	db.DB.Where("telegram_id = ?", id).First(&user)
	if user.Id == 0 {
		return nil
	}
	return user
}

func (db *UserStore) Create(e *entity.User) *entity.User {
	db.DB.Create(&e)
	return e
}

func (db *UserStore) Update(e *entity.User) *entity.User {
	db.DB.Where("id = ?", e.Id).Updates(&e)
	return e
}

func (db *UserStore) Delete(e *entity.User) {
	db.DB.Unscoped().Delete(&e)
}
