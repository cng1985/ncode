package model

import (
	"database/sql/driver"
	"fmt"
	"time"
)

type LocalDate string

func (d LocalDate) Value() (driver.Value, error) {
	if d == "" {
		return nil, nil
	}
	if _, err := time.Parse("2006-01-02", string(d)); err != nil {
		return nil, fmt.Errorf("invalid date")
	}
	return string(d), nil
}

func (d *LocalDate) Scan(value any) error {
	switch value := value.(type) {
	case nil:
		*d = ""
	case time.Time:
		*d = LocalDate(value.Format("2006-01-02"))
	case string:
		*d = LocalDate(value)
	case []byte:
		*d = LocalDate(string(value))
	default:
		return fmt.Errorf("unsupported date value %T", value)
	}
	return nil
}

func (LocalDate) GormDataType() string { return "date" }
