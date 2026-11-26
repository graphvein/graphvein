// Package rel holds the relation forms of the investigation relations, autoadd
// and createtable probes on postgres (rows GO-R1-R3, GO-K3, GO-P1).
package rel

// Company is a belongs-to target no call lists: AutoMigrate adds it.
type Company struct {
	ID   int
	Name string
}

// Employee belongs to Company (fk_employees_company).
type Employee struct {
	ID        int
	CompanyID int
	Company   Company
}

// CreditCard is the child of Owner's has-one.
type CreditCard struct {
	ID      uint
	Number  string
	OwnerID uint
}

// Owner has one CreditCard through a foreignKey tag (fk_owners_card).
type Owner struct {
	ID   uint
	Card CreditCard `gorm:"foreignKey:OwnerID"`
}

// Post has many PComments with actions (fk_posts_comments).
type Post struct {
	ID       uint
	Comments []PComment `gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`
}

// PComment is the child of Post's has-many.
type PComment struct {
	ID     uint
	PostID uint
}

// Lang is a many2many target that only the join table's constraint adds.
type Lang struct {
	ID   uint
	Code string `gorm:"uniqueIndex"`
}

// Speaker has many Langs through speaker_langs.
type Speaker struct {
	ID    uint
	Langs []Lang `gorm:"many2many:speaker_langs;"`
}

// Speaker2 names the join keys and the referenced column.
type Speaker2 struct {
	ID    uint
	Langs []Lang `gorm:"many2many:SpeakerLangTwo;foreignKey:ID;joinForeignKey:SpeakerRef;references:Code;joinReferences:LangCode"`
}

// Self belongs to itself.
type Self struct {
	ID        uint
	ManagerID *uint
	Manager   *Self
}

// Refd is referenced through a non-key column.
type Refd struct {
	ID   uint
	Code string `gorm:"unique"`
}

// Referrer belongs to Refd through references:Code.
type Referrer struct {
	ID       uint
	RefdCode string
	Refd     Refd `gorm:"foreignKey:RefdCode;references:Code"`
}

// Toy is the target of Cat's polymorphic relation (not read, row D).
type Toy struct {
	ID        uint
	OwnerID   uint
	OwnerType string
}

// Cat has many Toys through a polymorphic relation.
type Cat struct {
	ID   uint
	Toys []Toy `gorm:"polymorphic:Owner;"`
}

// NoFK's relation has constraint:-.
type NoFK struct {
	ID        uint
	CompanyID int
	Company   Company `gorm:"constraint:-"`
}

// Holder has many Pets; Pet's belongs-to Holder yields to it.
type Holder struct {
	ID   uint
	Pets []Pet `gorm:"constraint:OnDelete:CASCADE"`
}

// Pet belongs to Holder.
type Pet struct {
	ID       uint
	HolderID uint
	Holder   Holder
}

// Article and Tag declare the join table article_tags from both
// sides: its key order is unknown (open point O8).
type Article struct {
	ID   uint
	Tags []Tag `gorm:"many2many:article_tags;"`
}

// Tag is the other side of article_tags.
type Tag struct {
	ID       uint
	Articles []Article `gorm:"many2many:article_tags;"`
}

// Gadget has no key either relation could use.
type Gadget struct {
	ID   uint
	Name string
}

// Broken's relation has no foreign key GORM can guess: the call fails at
// Broken (GO-P1); Before is created first, After is not.
type Broken struct {
	ID     uint
	Gadget Gadget
}

// Before is migrated before Broken in the same call.
type Before struct {
	ID uint
}

// After is migrated after Broken in the same call.
type After struct {
	ID uint
}

// Queue is never created before Ticket.
type Queue struct {
	ID uint
}

// Ticket belongs to Queue; Migrator().CreateTable does not add Queue, so
// postgres fails the call (GO-P1).
type Ticket struct {
	ID      uint
	QueueID uint
	Queue   Queue
}

// Base is embedded with a relation field: the relation is not read (D).
type Base struct {
	CompanyID int
	Company   Company
}

// Branch embeds Base.
type Branch struct {
	ID uint
	Base
}
