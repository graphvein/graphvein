package gen

import "strings"

type R struct{}

func (r **R) Double()               {}
func (r strings.Builder) Qualified() {}
func (r R) Plain()                   {}
func (a, b R) Two()                  {}
