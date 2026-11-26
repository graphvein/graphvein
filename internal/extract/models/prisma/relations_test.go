package prisma

import (
	"reflect"
	"testing"
)

// TestRelations checks the relation-field accessor code-to-table linking
// reads (T018h CT-R2): every model's relation fields with the related
// model, list, implicit many-to-many sides and unread relations; ignored
// fields are left out; the group names its set and packages.
func TestRelations(t *testing.T) {
	sources := map[string][]byte{
		"package.json":      []byte(`{"name": "app", "devDependencies": {"prisma": "7.10.0"}}`),
		"package-lock.json": []byte(`{"lockfileVersion": 3, "packages": {"": {"name": "app"}, "node_modules/prisma": {"version": "7.10.0"}}}`),
		"prisma/schema.prisma": []byte(`datasource db {
  provider = "postgresql"
}

model User {
  id     Int     @id
  posts  Post[]
  groups Group[]
  odd    Post?   @relation("odd", fields: [oddId], references: [id], onDelete: Bogus)
  oddId  Int?
  hidden Post[]  @relation("hidden") @ignore
}

model Post {
  id       Int   @id
  author   User  @relation(fields: [authorId], references: [id])
  authorId Int
  odd      User[] @relation("odd")
  hider    User?  @relation("hidden", fields: [hiderId], references: [id])
  hiderId  Int?
}

model Group {
  id      Int    @id
  members User[]
}
`),
	}
	groups := Relations(sources)
	if len(groups) != 1 {
		t.Fatalf("groups %+v, want one", groups)
	}
	g := groups[0]
	if g.Set != "models/prisma/prisma/schema.prisma" || !reflect.DeepEqual(g.Packages, []string{"."}) {
		t.Errorf("set %q packages %v", g.Set, g.Packages)
	}
	want := map[string][]RelationField{
		"User": {
			{Name: "posts", Model: "Post", List: true},
			{Name: "groups", Model: "Group", List: true, ManyToMany: true},
			{Name: "odd", Model: "Post", Unread: true},
		},
		"Post": {
			{Name: "author", Model: "User"},
			{Name: "odd", Model: "User", List: true},
			{Name: "hider", Model: "User"},
		},
		"Group": {
			{Name: "members", Model: "User", List: true, ManyToMany: true},
		},
	}
	if !reflect.DeepEqual(g.Models, want) {
		t.Errorf("models\n got %+v\nwant %+v", g.Models, want)
	}
}
