package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestThirdPartyNotices is the licence check of scripts/test.sh: every Go
// module linked into the binary, every grammar and every web UI package has
// an allowed (or reviewed) licence, and the committed THIRD_PARTY_NOTICES
// equals the generated one. It runs offline.
func TestThirdPartyNotices(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	text, problems, err := Generate(root)
	var missing errModuleCache
	if errors.As(err, &missing) {
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			t.Fatal(err)
		}
		t.Skip(err.Error())
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
	current, err := os.ReadFile(filepath.Join(root, OutFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != text {
		t.Errorf("%s is stale; run go run ./scripts/notices from the repository root", OutFile)
	}
}

func TestClassify(t *testing.T) {
	cases := map[string][]string{
		"Permission is hereby granted, free of charge, to any person":                      {"MIT"},
		"Redistribution and use in source and binary forms ... Neither the name of Google": {"BSD-3-Clause"},
		"Redistribution and use in source and binary forms, with or without modification":  {"BSD-2-Clause"},
		"Apache License\n Version 2.0, January 2004":                                       {"Apache-2.0"},
		"Apache License v2.0 with LLVM Exceptions ... Apache License Version 2.0":          {"Apache-2.0 WITH LLVM-exception"},
		"Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted, provided that the above copyright notice and this permission notice appear in all copies.": {"ISC"},
		"Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted.":                                                                                           {"0BSD"},
		"Mozilla Public License Version 2.0":                         {"MPL-2.0"},
		"The GNU General Public License is a free, copyleft license": nil,
		"": nil,
	}
	for text, want := range cases {
		if got := classify(text); !reflect.DeepEqual(got, want) {
			t.Errorf("classify(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestExpressionIDs(t *testing.T) {
	got := expressionIDs("BSD-3-Clause AND blessing AND MIT")
	if strings.Join(got, ",") != "BSD-3-Clause,blessing,MIT" {
		t.Errorf("got %v", got)
	}
	if got := expressionIDs("Apache-2.0 WITH LLVM-exception"); len(got) != 1 {
		t.Errorf("got %v", got)
	}
}
