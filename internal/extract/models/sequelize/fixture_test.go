package sequelize

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
)

const fixtureDir = "../../../../testdata/fixtures/sequelize-models"

// fixtureSources reads the fixture as repository-relative sources; the
// README and goldens are expectations, not inputs.
func fixtureSources(t *testing.T) map[string]string {
	t.Helper()
	sources := map[string]string{}
	err := filepath.WalkDir(fixtureDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(fixtureDir, p)
		rel = filepath.ToSlash(rel)
		if rel == "README.md" || strings.HasSuffix(rel, ".golden.json") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sources[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sources
}

// linkFixture runs the claim pass over the fixture with the tag hook set.
func linkFixture(t *testing.T) ([]contract.Node, []contract.Diagnostic, *modeltest.Tags) {
	t.Helper()
	in := input(t, fixtureSources(t))
	tags := modeltest.NewTags()
	in.Tag = tags.Tag
	nodes, _, diags := Link(in)
	return nodes, diags, tags
}

// colLine renders a column's facts for the fixture expectations: state,
// name, type, nullable, primary_key, unique, index, default,
// server_default and server_default_text ("-" for unknown).
func colLine(n contract.Node) string {
	a := n.Attributes
	v := func(k string) string {
		if a[k] == nil {
			return "-"
		}
		return fmt.Sprint(a[k])
	}
	return strings.Join([]string{v("state"), v("name"), v("type"), v("nullable"), v("primary_key"), v("unique"), v("index"), v("default"), v("server_default"), v("server_default_text")}, " | ")
}

// tableLine renders a table's own facts.
func tableLine(n contract.Node) string {
	a := n.Attributes
	raw, _ := json.Marshal([]any{a["name"], a["schema"], a["set"], a["dialect"], a["verification"], a["column_set"], a["constraint_set"], a["primary_key"], a["unique_constraints"], a["indexes"]})
	return string(raw)
}

// fixtureTables are the claimed tables of the fixture: name, schema, group,
// dialect, verification, column_set, constraint_set, primary key, unique
// constraints and indexes.
var fixtureTables = map[string]string{
	"table:pg/src/models/Person.ts#Person":           `["People",null,"models/sequelize/pg/src/db.ts","postgres","oracle","complete","complete",["id"],[{"columns":["nick"],"name":null},{"columns":["region","zone"],"name":null},{"columns":["email"],"name":null}],[]]`,
	"table:pg/src/models/Mouse.ts#Mouse":             `["Mice",null,"models/sequelize/pg/src/db.ts","postgres","oracle","complete","complete",["id"],[],[{"columns":["name","region"],"method":"btree","name":"mice_name_region","partial":false,"unique":false,"where":null},{"columns":["serial"],"method":"btree","name":"mice_serial","partial":false,"unique":true,"where":null},{"columns":["rank"],"method":"btree","name":"mice_rank_desc","partial":false,"unique":false,"where":null},{"columns":["region"],"method":"btree","name":"mice_region","partial":true,"unique":false,"where":null},{"columns":["data"],"method":"gin","name":"mice_data","partial":false,"unique":false,"where":null}]]`,
	"table:pg/src/models/Archived.ts#ArchivedRecord": `["Archiveds",null,"models/sequelize/pg/src/db.ts","postgres","oracle","complete","complete",["uid"],[],[]]`,
	"table:pg/src/models/Category.ts#Category":       `["Categories","catalog","models/sequelize/pg/src/db.ts","postgres","oracle","partial","partial",null,[],[]]`,
	"table:pg/src/models/Settlement.ts#Settlement":   `["settlements",null,"models/sequelize/pg/src/db.ts","postgres","oracle","partial","partial",null,[],[]]`,
	"table:pg/src/models/Spread.ts#Spread":           `["Spreads",null,"models/sequelize/pg/src/db.ts","postgres","oracle","partial","partial",null,[],[]]`,
	"table:pg/src/models/Unread.ts#Unread":           `["Unread",null,"models/sequelize/pg/src/db.ts","postgres","oracle","partial","partial",null,[],[]]`,
	"table:pg/src/models/Wallet.ts#Wallet":           `["Wallets",null,"models/sequelize/pg/src/db.ts","postgres","oracle","partial","partial",null,[],[]]`,
	"table:pgdefine/src/db.js#BlogPost":              `["blog_posts",null,"models/sequelize/pgdefine/src/db.js","postgres","oracle","complete","complete",["id"],[],[{"columns":["read_count","title"],"method":"btree","name":"blog_posts_read_count_title","partial":false,"unique":false,"where":null}]]`,
	"table:pgdefine/src/reaction.js#Reaction":        `["Reaction",null,"models/sequelize/pgdefine/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgdefine/src/reaction.js#Report":          `["reports","reports","models/sequelize/pgdefine/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgdefine/src/reaction.js#Tally":           `["tallies",null,"models/sequelize/pgdefine/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgdefine/src/reaction.js#Setting":         `["settings",null,"models/sequelize/pgdefine/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:sqlite/models/area.js#t_area":             `["t_area",null,"models/sequelize/sqlite/models/index.js","sqlite","oracle","complete","complete",["id"],[{"columns":["name"],"name":null}],[]]`,
	"table:sqlite/models/point.js#t_point":           `["t_point",null,"models/sequelize/sqlite/models/index.js","sqlite","oracle","complete","complete",["id"],[],[]]`,
	"table:sqlite/models/pair.js#t_pair":             `["t_pair",null,"models/sequelize/sqlite/models/index.js","sqlite","oracle","complete","complete",["left_id","right_id"],[],[]]`,
	"table:sqlite/models/counter.js#t_counter":       `["t_counter",null,"models/sequelize/sqlite/models/index.js","sqlite","oracle","complete","complete",["id"],[],[]]`,
	"table:mapped/src/mysql.ts#Gadget":               `["Gadgets",null,"models/sequelize/mapped/src/mysql.ts","mysql","mapped","complete","complete",["id"],[{"columns":["serial"],"name":null}],[]]`,
	"table:pgassoc/src/models.js#Bowl":               `["bowls",null,"models/sequelize/pgassoc/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgassoc/src/models.js#Owner":              `["Owners",null,"models/sequelize/pgassoc/src/db.js","postgres","oracle","complete","complete",["id"],[{"columns":["code"],"name":null}],[]]`,
	"table:pgassoc/src/models.js#Pair":               `["Pairs",null,"models/sequelize/pgassoc/src/db.js","postgres","oracle","complete","complete",["a","b"],[],[]]`,
	"table:pgassoc/src/models.js#Pet":                `["Pets",null,"models/sequelize/pgassoc/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgassoc/src/models.js#Tag":                `["tags",null,"models/sequelize/pgassoc/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgassoc/src/models.js#Toy":                `["Toys",null,"models/sequelize/pgassoc/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgassoc/src/models.js#owner_pets":         `["owner_pets",null,"models/sequelize/pgassoc/src/db.js","postgres","oracle","complete","complete",["ownerKey","PetId"],[],[]]`,
	"table:pgassoc/src/models.js#tag_owners":         `["tag_owners",null,"models/sequelize/pgassoc/src/db.js","postgres","oracle","complete","complete",["tag_id","owner_id"],[],[]]`,
	"table:pgcycle/src/models.js#Badge":              `["Badges",null,"models/sequelize/pgcycle/src/db.js","postgres","oracle","complete","complete",["id"],[{"columns":["code"],"name":null}],[]]`,
	"table:pgcycle/src/models.js#Note":               `["Notes",null,"models/sequelize/pgcycle/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgcycle/src/models.js#Profile":            `["Profiles",null,"models/sequelize/pgcycle/src/db.js","postgres","oracle","complete","complete",["UserId"],[],[]]`,
	"table:pgcycle/src/models.js#Team":               `["Teams",null,"models/sequelize/pgcycle/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgcycle/src/models.js#User":               `["Users",null,"models/sequelize/pgcycle/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgfail/src/models.js#Home":                `["Homes",null,"models/sequelize/pgfail/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgfail/src/models.js#Shelf":               `["Shelves",null,"models/sequelize/pgfail/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgrefuse/src/models.js#Club":              `["Clubs",null,"models/sequelize/pgrefuse/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgrefuse/src/models.js#Member":            `["Members",null,"models/sequelize/pgrefuse/src/db.js","postgres","oracle","partial","partial",null,[],[]]`,
	"table:pgrefuse/src/models.js#Person":            `["People",null,"models/sequelize/pgrefuse/src/db.js","postgres","oracle","partial","partial",null,[],[]]`,
	"table:sqlite/models/area.js#t_area_counters":    `["t_area_counters",null,"models/sequelize/sqlite/models/index.js","sqlite","oracle","complete","complete",["t_area_id","t_counter_id"],[],[]]`,
	"table:pgconfig/src/models/Account.ts#Account":   `["Accounts","app","models/sequelize/pgconfig/src/lib/sequelize.ts","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:pgconfig/src/models/Session.ts#Session":   `["Sessions","app","models/sequelize/pgconfig/src/lib/sequelize.ts","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:mapped/src/mssql.ts#Widget":               `["Widgets",null,"models/sequelize/mapped/src/mssql.ts","mssql","mapped","partial","partial",null,[],[]]`,
	"table:cli/models/author.js#Author":              `["Authors",null,"models/sequelize/cli/models/index.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:cli/models/book.js#Book":                  `["Books",null,"models/sequelize/cli/models/index.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:cliclass/src/models/album.js#Album":       `["Albums",null,"models/sequelize/cliclass/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:cliclass/src/models/genre.js#Genre":       `["Genres",null,"models/sequelize/cliclass/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:cliclass/src/models/track.js#Track":       `["Tracks",null,"models/sequelize/cliclass/src/db.js","postgres","oracle","complete","complete",["id"],[],[]]`,
	"table:mapped/src/mssql.ts#User":                 `["Users",null,"models/sequelize/mapped/src/mssql.ts","mssql","mapped","complete","complete",["id"],[],[]]`,
	"table:mapped/src/mssql.ts#Memo":                 `["Memos",null,"models/sequelize/mapped/src/mssql.ts","mssql","mapped","complete","complete",["id"],[],[]]`,
	"table:mapped/src/mssql.ts#Org":                  `["Orgs",null,"models/sequelize/mapped/src/mssql.ts","mssql","mapped","complete","complete",["id"],[],[]]`,
	"table:mapped/src/mssql.ts#Group":                `["Groups",null,"models/sequelize/mapped/src/mssql.ts","mssql","mapped","complete","complete",["id"],[],[]]`,
}

// fixtureForeignKeys are the foreign-key lists of the columns that have
// one (rows SQ-A3, SQ-R1, SQ-R2); every other read column of a table
// whose lists are known has none.
var fixtureForeignKeys = map[string]string{
	"column:pgconfig/src/models/Session.ts#Session.AccountId":  `[{"column":"id","columns":["id"],"local_columns":["AccountId"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":"app","status":"resolved","table":"Accounts"}]`,
	"column:pgconfig/src/models/Session.ts#Session.accountRef": `[{"column":"id","columns":["id"],"local_columns":["accountRef"],"name":null,"on_delete":"CASCADE","on_update":"NO ACTION","schema":"app","status":"resolved","table":"Accounts"}]`,
	"column:pgconfig/src/models/Session.ts#Session.otherRef":   `[{"column":"id","columns":["id"],"local_columns":["otherRef"],"name":null,"on_delete":"NO ACTION","on_update":"NO ACTION","schema":"app","status":"resolved","table":"Accounts"}]`,
	"column:pg/src/models/Mouse.ts#Mouse.PersonId":             `[{"column":"id","columns":["id"],"local_columns":["PersonId"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":null,"status":"resolved","table":"People"}]`,
	"column:pgassoc/src/models.js#Bowl.ownerRef":               `[{"column":"id","columns":["id"],"local_columns":["owner_ref"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":null,"status":"resolved","table":"Owners"}]`,
	"column:pgassoc/src/models.js#Bowl.petRef":                 `[{"column":"id","columns":["id"],"local_columns":["pet_ref"],"name":null,"on_delete":"NO ACTION","on_update":"NO ACTION","schema":null,"status":"resolved","table":"Pets"}]`,
	"column:pgassoc/src/models.js#Bowl.plainRef":               `[{"column":"id","columns":["id"],"local_columns":["plain_ref"],"name":null,"on_delete":"CASCADE","on_update":"RESTRICT","schema":null,"status":"resolved","table":"Pets"}]`,
	"column:pgassoc/src/models.js#Pet.OwnerId":                 `[{"column":"id","columns":["id"],"local_columns":["OwnerId"],"name":null,"on_delete":"NO ACTION","on_update":"CASCADE","schema":null,"status":"resolved","table":"Owners"}]`,
	"column:pgassoc/src/models.js#Toy.OwnerId":                 `[{"column":"id","columns":["id"],"local_columns":["OwnerId"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":null,"status":"resolved","table":"Owners"}]`,
	"column:pgassoc/src/models.js#Toy.favouriteId":             `[{"column":"id","columns":["id"],"local_columns":["favouriteId"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":null,"status":"resolved","table":"Owners"}]`,
	"column:pgassoc/src/models.js#Toy.keeperId":                `[{"column":"id","columns":["id"],"local_columns":["keeperId"],"name":null,"on_delete":"CASCADE","on_update":"CASCADE","schema":null,"status":"resolved","table":"Owners"}]`,
	"column:pgassoc/src/models.js#Toy.makerId":                 `[{"column":"code","columns":["code"],"local_columns":["makerId"],"name":null,"on_delete":"CASCADE","on_update":"RESTRICT","schema":null,"status":"resolved","table":"Owners"}]`,
	"column:pgassoc/src/models.js#owner_pets.PetId":            `[{"column":"id","columns":["id"],"local_columns":["PetId"],"name":null,"on_delete":"CASCADE","on_update":"CASCADE","schema":null,"status":"resolved","table":"Pets"}]`,
	"column:pgassoc/src/models.js#owner_pets.ownerKey":         `[{"column":"id","columns":["id"],"local_columns":["ownerKey"],"name":null,"on_delete":"CASCADE","on_update":"CASCADE","schema":null,"status":"resolved","table":"Owners"}]`,
	"column:pgassoc/src/models.js#tag_owners.OwnerId":          `[{"column":"id","columns":["id"],"local_columns":["owner_id"],"name":null,"on_delete":"CASCADE","on_update":"CASCADE","schema":null,"status":"resolved","table":"Owners"}]`,
	"column:pgassoc/src/models.js#tag_owners.TagId":            `[{"column":"id","columns":["id"],"local_columns":["tag_id"],"name":null,"on_delete":"CASCADE","on_update":"CASCADE","schema":null,"status":"resolved","table":"tags"}]`,
	"column:pgcycle/src/models.js#Note.UserId":                 `[{"column":"id","columns":["id"],"local_columns":["UserId"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":null,"status":"resolved","table":"Users"}]`,
	"column:pgcycle/src/models.js#Team.ownerId":                `[{"column":"id","columns":["id"],"local_columns":["ownerId"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":null,"status":"resolved","table":"Users"}]`,
	"column:pgcycle/src/models.js#User.TeamId":                 `[{"column":"id","columns":["id"],"local_columns":["TeamId"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":null,"status":"resolved","table":"Teams"}]`,
	"column:sqlite/models/area.js#t_area_counters.tAreaId":     `[{"column":"id","columns":["id"],"local_columns":["t_area_id"],"name":null,"on_delete":"CASCADE","on_update":"CASCADE","schema":null,"status":"resolved","table":"t_area"}]`,
	"column:sqlite/models/area.js#t_area_counters.tCounterId":  `[{"column":"id","columns":["id"],"local_columns":["t_counter_id"],"name":null,"on_delete":"CASCADE","on_update":"CASCADE","schema":null,"status":"resolved","table":"t_counter"}]`,
	"column:sqlite/models/point.js#t_point.area_id":            `[{"column":"id","columns":["id"],"local_columns":["area_id"],"name":null,"on_delete":"CASCADE","on_update":"CASCADE","schema":null,"status":"resolved","table":"t_area"}]`,
	// The sequelize-cli forms: associations run by the
	// Object.keys forEach loop (cli) and the for...of Object.values loop over
	// static associate methods and a factory (cliclass).
	"column:cli/models/book.js#Book.AuthorId":             `[{"column":"id","columns":["id"],"local_columns":["AuthorId"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":null,"status":"resolved","table":"Authors"}]`,
	"column:cli/models/book.js#Book.editorId":             `[{"column":"id","columns":["id"],"local_columns":["editorId"],"name":null,"on_delete":"CASCADE","on_update":"CASCADE","schema":null,"status":"resolved","table":"Authors"}]`,
	"column:cliclass/src/models/album.js#Album.GenreId":   `[{"column":"id","columns":["id"],"local_columns":["GenreId"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":null,"status":"resolved","table":"Genres"}]`,
	"column:cliclass/src/models/album.js#Album.primaryId": `[{"column":"id","columns":["id"],"local_columns":["primaryId"],"name":null,"on_delete":"CASCADE","on_update":"RESTRICT","schema":null,"status":"resolved","table":"Genres"}]`,
	"column:cliclass/src/models/track.js#Track.AlbumId":   `[{"column":"id","columns":["id"],"local_columns":["AlbumId"],"name":null,"on_delete":"SET NULL","on_update":"CASCADE","schema":null,"status":"resolved","table":"Albums"}]`,
	// mssql: the first reference to Users keeps onDelete and
	// loses onUpdate, the second and the self-reference lose both.
	"column:mapped/src/mssql.ts#Memo.authorId": `[{"column":"id","columns":["id"],"local_columns":["authorId"],"name":null,"on_delete":"CASCADE","on_update":"NO ACTION","schema":null,"status":"resolved","table":"Users"}]`,
	"column:mapped/src/mssql.ts#Memo.editorId": `[{"column":"id","columns":["id"],"local_columns":["editorId"],"name":null,"on_delete":"NO ACTION","on_update":"NO ACTION","schema":null,"status":"resolved","table":"Users"}]`,
	"column:mapped/src/mssql.ts#Memo.parentId": `[{"column":"id","columns":["id"],"local_columns":["parentId"],"name":null,"on_delete":"NO ACTION","on_update":"NO ACTION","schema":null,"status":"resolved","table":"Memos"}]`,
	"column:mapped/src/mssql.ts#Group.OrgId":   `[{"column":"id","columns":["id"],"local_columns":["OrgId"],"name":null,"on_delete":"SET NULL","on_update":"NO ACTION","schema":null,"status":"resolved","table":"Orgs"}]`,
}

// fixtureColumns are the facts of selected columns, one or more per form
// (colLine order).
var fixtureColumns = map[string]string{
	// Person (postgres types, SQ-A1; column facts, SQ-A2; implicit
	// timestamps, SQ-O1).
	"Person.ts#Person.id":        "read | id | integer | false | true | false | false | server | true | -",
	"Person.ts#Person.nick":      "read | nick | character varying(255) | false | false | true | false | absent | false | -",
	"Person.ts#Person.title":     "read | title | character varying(40) | true | false | false | false | absent | false | -",
	"Person.ts#Person.code":      "read | code | character(4) | true | false | false | false | server | true | 'abcd'",
	"Person.ts#Person.bio":       "read | bio | text | true | false | false | false | absent | false | -",
	"Person.ts#Person.visits":    "read | visits | integer | false | false | false | false | server | true | 0",
	"Person.ts#Person.big":       "read | big | bigint | true | false | false | false | server | true | -3",
	"Person.ts#Person.small":     "read | small | smallint | true | false | false | false | absent | false | -",
	"Person.ts#Person.ratio":     "read | ratio | double precision | true | false | false | false | server | true | '1.5'",
	"Person.ts#Person.score":     "read | score | double precision | true | false | false | false | server | true | '2'",
	"Person.ts#Person.amount":    "read | amount | numeric(10,2) | true | false | false | false | server | true | 1.25",
	"Person.ts#Person.plain":     "read | plain | numeric | true | false | false | false | absent | false | -",
	"Person.ts#Person.active":    "read | active | boolean | true | false | false | false | server | true | true",
	"Person.ts#Person.bornAt":    "read | bornAt | timestamp with time zone | true | false | false | false | absent | false | -",
	"Person.ts#Person.bornOn":    "read | bornOn | date | true | false | false | false | absent | false | -",
	"Person.ts#Person.wakesAt":   "read | wakesAt | time without time zone | true | false | false | false | absent | false | -",
	"Person.ts#Person.token":     "read | token | uuid | true | false | false | false | - | false | -",
	"Person.ts#Person.prefs":     `read | prefs | json | true | false | false | false | server | true | '{"theme":"dark","sizes":[1,2]}'`,
	"Person.ts#Person.flags":     `read | flags | jsonb | true | false | false | false | server | true | '"{}"'`,
	"Person.ts#Person.avatar":    "read | avatar | bytea | true | false | false | false | absent | false | -",
	"Person.ts#Person.mood":      `read | mood | "enum_People_mood" | true | false | false | false | server | true | 'happy'`,
	"Person.ts#Person.level":     `read | level | "enum_People_level" | true | false | false | false | absent | false | -`,
	"Person.ts#Person.tags":      "read | tags | character varying(255)[] | true | false | false | false | absent | false | -",
	"Person.ts#Person.lucky":     "read | lucky | integer[] | true | false | false | false | absent | false | -",
	"Person.ts#Person.moods":     `read | moods | "enum_People_moods"[] | true | false | false | false | absent | false | -`,
	"Person.ts#Person.stay":      "read | stay | tstzrange | true | false | false | false | absent | false | -",
	"Person.ts#Person.region":    "read | region | character varying(255) | true | false | false | false | absent | false | -",
	"Person.ts#Person.email":     "read | email | character varying(255) | true | false | true | false | absent | false | -",
	"Person.ts#Person.seenAt":    "read | seenAt | timestamp with time zone | true | false | false | false | - | false | -",
	"Person.ts#Person.nothing":   "read | nothing | character varying(255) | true | false | false | false | absent | false | -",
	"Person.ts#Person.slug":      "read | slug | character varying(255) | true | false | false | false | server | true | -",
	"Person.ts#Person.stamp":     "read | stamp | timestamp with time zone | true | false | false | false | server | true | -",
	"Person.ts#Person.picked":    "read | picked | character varying(255) | true | false | false | false | - | false | -",
	"Person.ts#Person.legal":     "read | legal_name | character varying(255) | true | false | false | false | absent | false | -",
	"Person.ts#Person.createdAt": "read | createdAt | timestamp with time zone | false | false | false | false | absent | false | -",
	// Mouse (options, SQ-O1; indexes, SQ-O2).
	"Mouse.ts#Mouse.bornAt":    "read | bornAt | timestamp with time zone | false | false | false | false | absent | false | -",
	"Mouse.ts#Mouse.deletedAt": "read | deletedAt | timestamp with time zone | true | false | false | false | absent | false | -",
	"Mouse.ts#Mouse.version":   "read | version | integer | false | false | false | false | server | true | 0",
	"Mouse.ts#Mouse.serial":    "read | serial | character varying(255) | true | false | true | false | absent | false | -",
	"Mouse.ts#Mouse.rank":      "read | rank | integer | true | false | false | true | absent | false | -",
	"Mouse.ts#Mouse.region":    "read | region | character varying(255) | true | false | false | true | absent | false | -",
	"Mouse.ts#Mouse.data":      "read | data | jsonb | true | false | false | true | absent | false | -",
	"Mouse.ts#Mouse.name":      "read | name | character varying(255) | true | false | false | false | absent | false | -",
	// Archived (modelName; a user createdAt wins).
	"Archived.ts#ArchivedRecord.uid":       "read | uid | uuid | false | true | false | false | - | false | -",
	"Archived.ts#ArchivedRecord.createdAt": "read | createdAt | timestamp with time zone | true | false | false | false | absent | false | -",
	// Category (schema option; an ENUM in it is not compared).
	"Category.ts#Category.kind": "unknown | kind | - | - | - | - | - | - | - | -",
	"Category.ts#Category.id":   "read | id | integer | false | true | false | false | server | true | -",
	// Settlement (removeAttribute).
	"Settlement.ts#Settlement.id":    "unknown | id | - | - | - | - | - | - | - | -",
	"Settlement.ts#Settlement.group": "read | group | uuid | false | false | false | false | absent | false | -",
	// Spread (an unread entry: the default attributes are unknown).
	"Spread.ts#Spread.createdAt": "unknown | createdAt | - | - | - | - | - | - | - | -",
	"Spread.ts#Spread.title":     "read | title | character varying(255) | true | false | false | false | absent | false | -",
	// Unread (unknown columns, open points O4 and O5).
	"Unread.ts#Unread.shown": "unknown | shown | - | - | - | - | - | - | - | -",
	"Unread.ts#Unread.price": "unknown | price | - | - | - | - | - | - | - | -",
	"Unread.ts#Unread.cents": "unknown | cents | - | - | - | - | - | - | - | -",
	"Unread.ts#Unread.raw":   "unknown | raw | - | - | - | - | - | - | - | -",
	"Unread.ts#Unread.wide":  "unknown | wide | - | - | - | - | - | - | - | -",
	"Unread.ts#Unread.ident": "unknown | ident | - | - | - | - | - | - | - | -",
	"Unread.ts#Unread.state": "read | state | character varying(255) | true | false | false | false | - | - | -",
	// Wallet (attribute helpers, row SQ-A1 form "helper member", open point O4).
	"Wallet.ts#Wallet.currency": "read | currency | character varying(3) | false | false | false | false | server | true | 'USD'",
	"Wallet.ts#Wallet.country":  "read | country | character varying(2) | true | false | false | false | - | - | -",
	"Wallet.ts#Wallet.flagged":  "unknown | flagged | - | - | - | - | - | - | - | -",
	"Wallet.ts#Wallet.balance":  "read | balance | integer | false | false | false | false | absent | false | -",
	"Wallet.ts#Wallet.fee":      "read | fee | numeric(10,4) | false | false | false | false | server | true | 0",
	"Wallet.ts#Wallet.rate":     "unknown | rate | - | - | - | - | - | - | - | -",
	"Wallet.ts#Wallet.label":    "unknown | label | - | - | - | - | - | - | - | -",
	// pgdefine (global define: underscored and paranoid).
	"db.js#BlogPost.readCount":       "read | read_count | integer | false | false | false | false | server | true | 0",
	"db.js#BlogPost.title":           "read | title | character varying(255) | true | false | false | false | absent | false | -",
	"db.js#BlogPost.createdAt":       "read | created_at | timestamp with time zone | false | false | false | false | absent | false | -",
	"db.js#BlogPost.deletedAt":       "read | deleted_at | timestamp with time zone | true | false | false | false | absent | false | -",
	"reaction.js#Reaction.emojiCode": "read | emoji_code | character varying(8) | true | false | false | false | absent | false | -",
	"reaction.js#Report.body":        "read | body | text | true | false | false | false | absent | false | -",
	"reaction.js#Tally.id":           "read | id | bigint | false | true | false | false | server | true | -",
	"reaction.js#Setting.label":      "read | label | character varying(255) | true | false | false | false | server | true | 'untitled'",
	"reaction.js#Setting.body":       "read | body | text | true | false | false | false | server | true | 'empty'",
	"reaction.js#Setting.tiny":       "read | tiny | smallint | true | false | false | false | server | true | 7",
	"reaction.js#Setting.word":       `read | word | json | true | false | false | false | server | true | '"x"'`,
	"reaction.js#Setting.count":      "read | count | json | true | false | false | false | server | true | '3'",
	"reaction.js#Setting.weight":     "read | weight | jsonb | true | false | false | false | server | true | '4'",
	"reaction.js#Setting.on":         "read | on | json | true | false | false | false | server | true | 'true'",
	"reaction.js#Setting.off":        "read | off | jsonb | true | false | false | false | server | true | 'false'",
	"reaction.js#Setting.shape":      `read | shape | jsonb | true | false | false | false | server | true | '{"a":1}'`,
	// sqlite.
	"area.js#t_area.id":         "read | id | UUID | true | true | false | false | - | false | -",
	"area.js#t_area.name":       "read | name | VARCHAR(255) | false | false | true | false | absent | false | -",
	"area.js#t_area.visible":    "read | visible | TINYINT(1) | false | false | false | false | server | true | 0",
	"area.js#t_area.zoom":       "read | zoom | DOUBLE PRECISION | true | false | false | false | server | true | '1.5'",
	"area.js#t_area.kind":       "read | kind | TEXT | true | false | false | false | server | true | 'room'",
	"area.js#t_area.settings":   "read | settings | JSON | true | false | false | false | server | true | '{}'",
	"area.js#t_area.since":      "read | since | DATE | true | false | false | false | absent | false | -",
	"area.js#t_area.touched":    "read | touched | DATETIME | true | false | false | false | server | true | -",
	"area.js#t_area.created_at": "read | created_at | DATETIME | false | false | false | false | absent | false | -",
	"point.js#t_point.id":       "read | id | INTEGER | true | true | false | false | - | - | -",
	"point.js#t_point.area_id":  "read | area_id | UUID | false | false | false | false | absent | false | -",
	"pair.js#t_pair.left_id":    "read | left_id | INTEGER | false | true | false | false | absent | false | -",
	"pair.js#t_pair.weight":     "read | weight | INTEGER | true | false | false | false | server | true | 1",
	"counter.js#t_counter.id":   "read | id | INTEGER | true | true | false | false | - | - | -",
	"counter.js#t_counter.hits": "read | hits | BIGINT | false | false | false | false | server | true | 0",
	// mapped dialects (SQ-V6).
	"mysql.ts#Gadget.serial":  "read | serial | VARCHAR(30) | false | false | true | false | absent | false | -",
	"mysql.ts#Gadget.enabled": "read | enabled | TINYINT(1) | true | false | false | false | server | true | -",
	"mysql.ts#Gadget.token":   "read | token | CHAR(36) BINARY | true | false | false | false | absent | false | -",
	"mysql.ts#Gadget.notes":   "read | notes | TEXT | true | false | false | false | absent | false | -",
	"mysql.ts#Gadget.size":    "read | size | ENUM('s', 'm') | true | false | false | false | absent | false | -",
	"mysql.ts#Gadget.id":      "read | id | INTEGER | false | true | false | false | - | - | -",
	"mssql.ts#Widget.label":   "read | label | NVARCHAR(255) | false | false | false | false | absent | false | -",
	"mssql.ts#Widget.count":   "read | count | INTEGER | - | false | false | false | server | true | -",
	"mssql.ts#Widget.enabled": "read | enabled | BIT | true | false | false | false | absent | false | -",
	"mssql.ts#Widget.size":    "unknown | size | - | - | - | - | - | - | - | -",
}

// fixtureForeignKeysUnknown are columns whose foreign-key list is unknown
// (the cyclic rule; refused association forms).
var fixtureForeignKeysUnknown = []string{
	"pgcycle/src/models.js#Profile.UserId",
	"pgcycle/src/models.js#Badge.TeamId",
	"pgrefuse/src/models.js#Member.role",
	"pgrefuse/src/models.js#Member.createdAt",
	"pgrefuse/src/models.js#Member.updatedAt",
}

// fixtureFailures are the provable failures (row SQ-P1) the fixture holds:
// file and a part of the message.
var fixtureFailures = [][2]string{
	{"pgfail/src/models.js", "Broken.homeRef: references Homes (nope)"},
	{"pgfail/src/models.js", "Away: belongsTo(Home) from a model in schema other"},
	{"pgfail/src/models.js", `Lost: the targetKey "missing"`},
	// A targetKey that is not unique, and a table whose
	// references name the table that is therefore not created.
	{"pgfail/src/models.js", "Rack.byCodeId: references Shelves (code), which is neither the primary key nor a unique column"},
	{"pgfail/src/models.js", "Bin.rackRef: references Racks, which sync does not create"},
}

// TestSequelizeFixture runs the claim pass over the fixture: the kit's
// claim checks (modeltest.CheckClaims), every claimed table and the facts
// of one or more columns per form, every foreign-key list (the lists
// fixtureForeignKeys names, unknown for fixtureForeignKeysUnknown, empty
// for every other read column of a table whose lists are known), the
// markers for the forms that are not read and the provable failures.
func TestSequelizeFixture(t *testing.T) {
	nodes, diags, _ := linkFixture(t)
	modeltest.CheckClaims(t, nodes, models.ClaimFacts)
	tables := map[string]bool{}
	fkSeen := map[string]bool{}
	for _, n := range nodes {
		switch n.Kind {
		case "table":
			tables[n.Identity] = true
			// A partial set always carries a table-level
			// reason.
			reasons, _ := n.Attributes["reasons"].([]models.Reason)
			for _, set := range []string{"column_set", "constraint_set"} {
				if n.Attributes[set] != "partial" {
					continue
				}
				found := false
				for _, r := range reasons {
					found = found || r.Fact != nil && *r.Fact == set
				}
				if !found {
					t.Errorf("%s: %s partial without a table reason", n.Identity, set)
				}
			}
			want, ok := fixtureTables[n.Identity]
			if !ok {
				t.Errorf("unexpected table %s", n.Identity)
				continue
			}
			if got := tableLine(n); got != want {
				t.Errorf("%s:\n got %s\nwant %s", n.Identity, got, want)
			}
		case "column":
			for suffix, want := range fixtureColumns {
				if strings.HasSuffix(n.Identity, "/"+suffix) {
					if got := colLine(n); got != want {
						t.Errorf("%s:\n got %s\nwant %s", n.Identity, got, want)
					}
				}
			}
			raw, _ := json.Marshal(n.Attributes["foreign_keys"])
			id := strings.TrimPrefix(n.Identity, "column:")
			if want, ok := fixtureForeignKeys[n.Identity]; ok {
				fkSeen[n.Identity] = true
				if string(raw) != want {
					t.Errorf("%s foreign keys:\n got %s\nwant %s", n.Identity, raw, want)
				}
				continue
			}
			unknown := false
			for _, u := range fixtureForeignKeysUnknown {
				if id == u {
					unknown = true
					fkSeen[u] = true
				}
			}
			switch {
			case unknown && string(raw) != "null":
				t.Errorf("%s: foreign keys %s, want unknown", n.Identity, raw)
			case !unknown && n.Attributes["state"] == "read" && string(raw) != "[]":
				t.Errorf("%s: foreign keys %s, want []", n.Identity, raw)
			}
		}
	}
	for id := range fixtureTables {
		if !tables[id] {
			t.Errorf("table %s not claimed", id)
		}
	}
	for id := range fixtureForeignKeys {
		if !fkSeen[id] {
			t.Errorf("column %s not emitted", id)
		}
	}
	for _, u := range fixtureForeignKeysUnknown {
		if !fkSeen[u] {
			t.Errorf("column %s not emitted", u)
		}
	}
	for suffix := range fixtureColumns {
		found := false
		for _, n := range nodes {
			found = found || n.Kind == "column" && strings.HasSuffix(n.Identity, "/"+suffix)
		}
		if !found {
			t.Errorf("column %s not emitted", suffix)
		}
	}
	for _, n := range nodes {
		if n.Kind == "column" && strings.Contains(n.Identity, "#Person.fullName") {
			t.Error("the VIRTUAL attribute fullName has a column")
		}
	}
	for _, want := range []struct{ code, file, part string }{
		{codeNotProven, "legacy/package.json", "sequelize 5.22.5"},
		{codeUnread, "decorators/package.json", "sequelize-typescript"},
		{codeNotClaimed, "decorators/src/Tagged.ts", "has decorators"},
		{codeUnread, "pg/src/models/helpers.ts", "inside a function"},
		{codeNotClaimed, "pgrefuse/src/models.js", `belongsToMany(...) over "club_links": a self-association`},
		// Instance hooks: the group claims nothing.
		{codeNotProven, "pghooks/src/option.js", "the options hold a hooks key"},
		{codeNotProven, "pghooks/src/added.js", "sequelize.addHook(...) registers a hook"},
		{codeNotProven, "pghooks/src/method.js", "sequelize.beforeDefine(...) registers a hook"},
		// Load-time throws: a definition after the throw in
		// its module, and one in a module whose order is not proven.
		{codeNotClaimed, "pgfail/src/models.js", "SQ-P1: it runs after pgfail/src/models.js:29, which throws while the models load"},
		{codeNotClaimed, "pgfail/src/late.js", "SQ-P1: pgfail/src/models.js:29 throws while the models load, and this definition is not proven to run before it"},
		// A model of another package on the pgrefuse
		// instance.
		{codeUnread, "pgrefuse/plugin/extra.js", "a model of another package (pgrefuse/plugin) reaches Sequelize instance pgrefuse/src/db.js"},
		// A class + init inside a CommonJS factory.
		{codeUnread, "cli/models/legacy.js", "Legacy.init(...) inside a function (a class model built in a CommonJS factory"},
	} {
		if !hasDiag(diags, want.code, want.file, want.part) {
			t.Errorf("no %s on %s with %q", want.code, want.file, want.part)
		}
	}
	// Every marker about a definition that is not read ends with its
	// group's table-list marker (the server reads it).
	for _, d := range diags {
		if d.Code == codeUnread && strings.Contains(d.Message, "pg/src/models/helpers.ts") && !strings.HasSuffix(d.Message, listMarker("models/sequelize/pg/src/db.ts")) {
			t.Errorf("marker without the table-list suffix: %s", d.Message)
		}
	}
	failures := 0
	for _, d := range diags {
		if d.Code != codeImportFailure {
			continue
		}
		failures++
		found := false
		for _, f := range fixtureFailures {
			found = found || d.File == f[0] && strings.Contains(d.Message, f[1])
		}
		if !found {
			t.Errorf("unexpected failure: %s", d.Message)
		}
	}
	if failures != len(fixtureFailures) {
		t.Errorf("%d failures, want %d", failures, len(fixtureFailures))
	}
}
