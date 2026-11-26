package python

import "testing"

// TestFastAPIRequirement checks the manifest lower bounds the route shape
// reads (rows SH-F20 and SH-F26 of route-shape-fastapi.md): only a lower
// bound every allowed install meets proves a version.
func TestFastAPIRequirement(t *testing.T) {
	cases := []struct {
		name, text string
		want       Version
		declared   bool
		ok         bool
	}{
		{"pep508 lower bound", "[project]\ndependencies = [\"fastapi[standard]<1.0.0,>=0.114.2\"]\n", Version{0, 114, 2}, true, true},
		{"exact", "[project]\ndependencies = [\"fastapi==0.89.1\"]\n", Version{0, 89, 1}, true, true},
		{"compatible", "[project]\ndependencies = [\"fastapi~=0.115\"]\n", Version{0, 115, 0}, true, true},
		{"slim", "[project]\ndependencies = [\"fastapi-slim>=0.120\"]\n", Version{0, 120, 0}, true, true},
		{"upper bound only", "[project]\ndependencies = [\"fastapi<0.100\"]\n", Version{}, true, false},
		{"no specifier", "[project]\ndependencies = [\"fastapi\"]\n", Version{}, true, false},
		{"marker", "[project]\ndependencies = [\"fastapi>=0.115; python_version >= '3.9'\"]\n", Version{}, true, false},
		{"other package", "[project]\ndependencies = [\"fastapi-users>=10\"]\n", Version{}, false, false},
		{"poetry caret", "[tool.poetry.dependencies]\nfastapi = \"^0.110.0\"\n", Version{0, 110, 0}, true, true},
		{"poetry table", "[tool.poetry.dependencies]\nfastapi = { version = \">=0.95\", extras = [\"all\"] }\n", Version{0, 95, 0}, true, true},
		{"none", "[project]\nname = \"x\"\n", Version{}, false, false},
		// SHFR-17: the lowest alternative of a Poetry `||` constraint.
		{"poetry alternatives", "[tool.poetry.dependencies]\nfastapi = \"^0.110 || ^0.80\"\n", Version{0, 80, 0}, true, true},
		{"poetry single bar", "[tool.poetry.dependencies]\nfastapi = \">=0.95,<1.0 | ^0.90\"\n", Version{0, 90, 0}, true, true},
		{"poetry alternative unread", "[tool.poetry.dependencies]\nfastapi = \"^0.110 || <0.80\"\n", Version{}, true, false},
		{"pep508 alternative", "[project]\ndependencies = [\"fastapi>=0.110 || >=0.80\"]\n", Version{}, true, false},
		// SHFR-18: two sources take the lower bound; uv overrides and
		// constraints, and pre-release bounds, are not read.
		{"project and poetry", "[project]\ndependencies = [\"fastapi>=0.115\"]\n[tool.poetry.dependencies]\nfastapi = \"^0.95\"\n", Version{0, 95, 0}, true, true},
		{"poetry and project", "[project]\ndependencies = [\"fastapi>=0.80\"]\n[tool.poetry.dependencies]\nfastapi = \"^0.110\"\n", Version{0, 80, 0}, true, true},
		{"several lines of one source", "[project]\ndependencies = [\"fastapi>=0.80\", \"fastapi[standard]>=0.110\"]\n", Version{0, 110, 0}, true, true},
		{"uv override", "[project]\ndependencies = [\"fastapi>=0.115\"]\n[tool.uv]\noverride-dependencies = [\"fastapi==0.70.0\"]\n", Version{}, true, false},
		{"uv constraint", "[project]\ndependencies = [\"fastapi>=0.115\"]\n[tool.uv]\nconstraint-dependencies = [\"fastapi<0.90\"]\n", Version{}, true, false},
		{"uv other package", "[project]\ndependencies = [\"fastapi>=0.115\"]\n[tool.uv]\noverride-dependencies = [\"pydantic==2.7\"]\n", Version{0, 115, 0}, true, true},
		{"pre-release", "[project]\ndependencies = [\"fastapi>=0.89.0rc1\"]\n", Version{}, true, false},
		{"dev release", "[project]\ndependencies = [\"fastapi>=0.89.0.dev1\"]\n", Version{}, true, false},
		{"wildcard", "[project]\ndependencies = [\"fastapi==0.115.*\"]\n", Version{0, 115, 0}, true, true},
	}
	for _, c := range cases {
		v, declared, ok := fastAPIRequirement(c.text)
		if v != c.want || declared != c.declared || ok != c.ok {
			t.Errorf("%s: got %v %v %v, want %v %v %v", c.name, v, declared, ok, c.want, c.declared, c.ok)
		}
	}
}

// TestPydanticRequirement checks the Pydantic lower bound row SH-F22
// reads: a nullable field without a default is required only when every
// install the manifest allows is Pydantic 2 or later.
func TestPydanticRequirement(t *testing.T) {
	cases := []struct {
		name, text string
		want       Version
		declared   bool
		ok         bool
	}{
		{"pydantic 2", "[project]\ndependencies = [\"pydantic<3.0.0,>=2.7.0\"]\n", Version{2, 7, 0}, true, true},
		{"extras", "[project]\ndependencies = [\"pydantic[email]>=2.0\"]\n", Version{2, 0, 0}, true, true},
		{"pydantic 1 pin", "[project]\ndependencies = [\"pydantic==1.10.13\"]\n", Version{1, 10, 13}, true, true},
		{"either major", "[project]\ndependencies = [\"pydantic>=1.10,<3\"]\n", Version{1, 10, 0}, true, true},
		{"poetry", "[tool.poetry.dependencies]\npydantic = \"^2.4\"\n", Version{2, 4, 0}, true, true},
		{"no bound", "[project]\ndependencies = [\"pydantic\"]\n", Version{}, true, false},
		{"other package", "[project]\ndependencies = [\"pydantic-settings>=2.1\"]\n", Version{}, false, false},
		{"only through fastapi", "[project]\ndependencies = [\"fastapi>=0.115\"]\n", Version{}, false, false},
	}
	for _, c := range cases {
		v, declared, ok := packageRequirement(c.text, pydanticName)
		if v != c.want || declared != c.declared || ok != c.ok {
			t.Errorf("%s: got %v %v %v, want %v %v %v", c.name, v, declared, ok, c.want, c.declared, c.ok)
		}
	}
}
