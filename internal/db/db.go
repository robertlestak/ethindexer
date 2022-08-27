package db

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	DB       *gorm.DB
	DBReader *gorm.DB
)

func initPrimary() error {
	dsn := fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_PASSWORD"),
	)
	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	return nil
}

func initReader() error {
	if os.Getenv("DB_HOST_READER") == "" {
		return nil
	}
	dsn := fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s sslmode=disable",
		os.Getenv("DB_HOST_READER"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_PASSWORD"),
	)
	var err error
	DBReader, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	return nil
}

func Init() error {
	if err := initPrimary(); err != nil {
		return err
	}
	if err := initReader(); err != nil {
		return err
	}
	if DBReader == nil {
		DBReader = DB
	}
	return nil
}

func Ping(db *gorm.DB) error {
	d, derr := db.DB()
	if derr != nil {
		return derr
	}
	return d.Ping()
}

func Healthcheck() error {
	if err := Ping(DB); err != nil {
		return err
	}
	if err := Ping(DBReader); err != nil {
		return err
	}
	return nil
}

func Healthchecker() error {
	for {
		if err := Healthcheck(); err != nil {
			log.Fatal(err)
			return err
		}
		time.Sleep(time.Second * 10)
	}
}

func Paginate(page, pageSize int) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if page == 0 {
			page = 1
		}

		switch {
		case pageSize > 1000:
			pageSize = 1000
		case pageSize <= 0:
			pageSize = 10
		}

		offset := (page - 1) * pageSize
		return db.Offset(offset).Limit(pageSize)
	}
}
