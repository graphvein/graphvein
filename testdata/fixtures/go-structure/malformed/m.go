package malformed

import (
	_ "example.test/structure/../lib"
	_ "example.test/structure/./lib"
	_ "example.test/structure//lib"
	_ "example.test/structure/lib/"
	_ "fmt/../os"
)
