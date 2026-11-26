package fixture

func TestOnly() { local() }
func TestRun() { Run(); TestOnly() }
