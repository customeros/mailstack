package models

import (
	"time"
)

type Domain struct {
	ID          uint64    `gorm:"primary_key;autoIncrement" json:"id"`
	Tenant      string    `gorm:"column:tenant;type:varchar(255);NOT NULL" json:"tenant"`
	Domain      string    `gorm:"column:domain;type:varchar(255);NOT NULL;uniqueIndex" json:"domain"`
	Configured  bool      `gorm:"column:configured;type:boolean;NOT NULL;DEFAULT:false" json:"configured"`
	CreatedAt   time.Time `gorm:"column:created_at;type:timestamp;DEFAULT:current_timestamp" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:timestamp;DEFAULT:current_timestamp" json:"updatedAt"`
	Active      bool      `gorm:"column:active;type:boolean;NOT NULL;DEFAULT:true" json:"active"`
	DkimPublic  string    `gorm:"column:dkim_public;type:text" json:"dkimPublic"`
	DkimPrivate string    `gorm:"column:dkim_private;type:text" json:"dkimPrivate"`
}

func (Domain) TableName() string {
	return "domains"
}
