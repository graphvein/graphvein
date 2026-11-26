//go:build ignore

package main

// gothinkster/golang-gin-realworld-example-app at a41289b2 (gorm 1.25.12,
// gorm.io/driver/sqlite 1.5.7; the oracle uses glebarez):
// hello.go's Migrate expands users.AutoMigrate (users/models.go:48-49)
// before its own five calls, each call one model. Its dialect is sqlite;
// the postgres run is cross-dialect (only the dialect-independent facts
// are compared).

import (
	"example.invalid/gormoracle/corpus/realworld/articles"
	"example.invalid/gormoracle/corpus/realworld/users"
)

func init() {
	register(run{Name: "realworld", Repo: "realworld-gin", Group: "models/gorm/.", Dialects: []string{"sqlite", "postgres"},
		CrossDialect: map[string]bool{"postgres": true},
		Steps: []step{
			{Site: "users/models.go:48", Models: []any{&users.UserModel{}}},
			{Site: "users/models.go:49", Models: []any{&users.FollowModel{}}},
			{Site: "hello.go:17", Models: []any{&articles.ArticleModel{}}},
			{Site: "hello.go:18", Models: []any{&articles.TagModel{}}},
			{Site: "hello.go:19", Models: []any{&articles.FavoriteModel{}}},
			{Site: "hello.go:20", Models: []any{&articles.ArticleUserModel{}}},
			{Site: "hello.go:21", Models: []any{&articles.CommentModel{}}},
		}})
}
