package model

import (
	"database/sql"
	"database/sql/driver"
	"time"

	"gorm.io/gorm"
)

// Kind and Level are named basic types (GO-T1).
type Kind string
type Level int8

// Money is a Valuer: a GORM custom type (GO-T1 refusal).
type Money int64

// Value implements driver.Valuer.
func (m Money) Value() (driver.Value, error) { return int64(m), nil }

// Blob implements GormDataType (GO-T1 refusal).
type Blob []byte

// GormDataType names the type GORM writes.
func (Blob) GormDataType() string { return "blob" }

type TypeProbe struct {
	ID           uint
	B            bool
	BP           *bool
	I            int
	I8           int8
	I16          int16
	I32          int32
	I64          int64
	U            uint
	U8           uint8
	U16          uint16
	U32          uint32
	U64          uint64
	F32          float32
	F64          float64
	S            string
	SP           *string
	T            time.Time
	TP           *time.Time
	Raw          []byte
	K            Kind
	L            Level
	NS           sql.NullString
	NI           sql.NullInt64
	NI32         sql.NullInt32
	NI16         sql.NullInt16
	NBy          sql.NullByte
	NF           sql.NullFloat64
	NB           sql.NullBool
	NT           sql.NullTime
	Del          gorm.DeletedAt
	Sized        string    `gorm:"size:64"`
	SizedInt     int       `gorm:"size:32"`
	SizedBytes   []byte    `gorm:"size:100"`
	Dec          float64   `gorm:"precision:10;scale:2"`
	DecP         float64   `gorm:"precision:8"`
	TPrec        time.Time `gorm:"precision:6"`
	TypeVarchar  string    `gorm:"type:varchar(40)"`
	TypeText     string    `gorm:"type:text"`
	TypeBytes    string    `gorm:"type:bytes"`
	TypeGenInt   string    `gorm:"type:Int"`
	TypeTimeTag  time.Time `gorm:"type:timestamp"`
	TypeVarSize  string    `gorm:"type:varchar(20);size:99"`
	Col          string    `gorm:"column:custom_col"`
	ColUpper     string    `gorm:"COLUMN:UpperCol"`
	NotNull      string    `gorm:"not null"`
	NotNull2     int       `gorm:"NOTNULL"`
	NotNullFalse int       `gorm:"not null:false"`
	DefInt       int       `gorm:"default:7"`
	DefStrQ      string    `gorm:"default:'hello'"`
	DefStr       string    `gorm:"default:world"`
	DefBool      bool      `gorm:"default:true"`
	DefFloat     float64   `gorm:"default:1.5"`
	DefNull      *string   `gorm:"default:null"`
	DefCurrent   time.Time `gorm:"default:CURRENT_TIMESTAMP"`
	DefParen     string    `gorm:"default:(lower('X'))"`
	DefNone      string    `gorm:"default:(-)"`
	DefEmpty     string    `gorm:"default:''"`
	DefNotNull   int       `gorm:"default:0;not null"`
	Comment      string    `gorm:"comment:hello"`
	ReadOnly     string    `gorm:"->"`
	ReadOnlyF    string    `gorm:"->:false;<-:create"`
	WriteCreate  string    `gorm:"<-:create"`
	Ignored      string    `gorm:"-"`
	IgnoredMig   string    `gorm:"-:migration"`
	IgnoredAll   string    `gorm:"-:all"`
	unexported   string
	Ser          map[string]any `gorm:"serializer:json"`
	Uniq         string         `gorm:"unique"`
	UniqIdx      string         `gorm:"unique;uniqueIndex"`
	CreatedAt    time.Time
	UpdatedAt    int64
	Created2     int64 `gorm:"autoCreateTime:milli"`
}

type IndexProbe struct {
	ID     uint   `gorm:"primaryKey"`
	A      string `gorm:"index"`
	B      string `gorm:"index:idx_named"`
	C      string `gorm:"uniqueIndex"`
	D      string `gorm:"uniqueIndex:uix_named"`
	E      string `gorm:"index:idx_comp,priority:2"`
	F      string `gorm:"index:idx_comp,priority:1"`
	G      string `gorm:"index:,unique"`
	H      string `gorm:"index:idx_sorted,sort:desc"`
	I      string `gorm:"index:idx_partial,where:i <> ''"`
	K      string `gorm:"index:,composite:kk"`
	L      string `gorm:"index:,composite:kk"`
	M      string `gorm:"uniqueIndex:uix_len,length:10"`
	N      string `gorm:"index:idx_expr,expression:lower(n)"`
	OIDCID string `gorm:"index"`
	P      string `gorm:"column:p_col;index"`
	Q      string `gorm:"index:idx_q1;index:idx_q2"`
	R      string `gorm:"INDEX:idx_upper_key"`
	S1     string `gorm:"index:idx_same"`
	S2     string `gorm:"index:idx_same"`
	U      string `gorm:"uniqueIndex:uix_comp"`
	V      string `gorm:"uniqueIndex:uix_comp"`
}

type CompositePK struct {
	A string `gorm:"primaryKey"`
	B int    `gorm:"primaryKey;autoIncrement:false"`
	C string
}

type StringPK struct {
	Code string `gorm:"primaryKey"`
	Name string
}

type NoIDTable struct {
	Name  string
	Value int
}

type IDNotFirst struct {
	Name string
	ID   int64
}

type ExplicitPKNotID struct {
	ID  uint
	Key int `gorm:"primaryKey"`
}

type ColumnID struct {
	Ident uint `gorm:"column:id"`
	Name  string
}

type StringID struct {
	ID   string
	Name string
}

type PKNoAI struct {
	ID   uint `gorm:"primaryKey;autoIncrement:false"`
	Name string
}

type CompositeIntPK struct {
	A uint `gorm:"primaryKey"`
	B uint `gorm:"primaryKey"`
	C string
}

// Base and Audit are embedded (GO-F1).
type Base struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
}

type Audit struct {
	By string `gorm:"size:10"`
	At time.Time
}

type Embeds struct {
	Base
	Audit    `gorm:"embedded;embeddedPrefix:audit_"`
	Named    Audit  `gorm:"embedded"`
	Prefixed Audit  `gorm:"embedded;embeddedPrefix:p_"`
	Ptr      *Audit `gorm:"embedded;embeddedPrefix:ptr_"`
	Name     string
}

type WithModel struct {
	gorm.Model
	Name string
}

// EmbedTagged's embedding field has a tag GORM copies into every embedded
// field (GO-F1 refusal).
type EmbedTagged struct {
	ID    uint
	Audit `gorm:"not null"`
}

// PGOnly holds the postgres-only forms (index type, raw types, generated).
type PGOnly struct {
	ID    uint   `gorm:"primaryKey"`
	J     string `gorm:"index:idx_pg_type,type:btree"`
	G     string `gorm:"index:idx_pg_hash,type:hash"`
	U     string `gorm:"type:uuid;default:gen_random_uuid()"`
	Ident int64  `gorm:"generated:identity"`
	Arr   string `gorm:"type:text[]"`
	Calc  int    `gorm:"generated:ident * 2"`
	JS    string `gorm:"type:jsonb"`
	Odd   string `gorm:"type:inet"`
}

type CustomProbe struct {
	ID  uint
	Mon Money
	Bl  Blob
}
