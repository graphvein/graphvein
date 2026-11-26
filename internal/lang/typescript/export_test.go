package typescript

// WriteStructureExtraFiles lets the external test package write the
// fixture cases the repository cannot store (structureExtraFiles).
var WriteStructureExtraFiles = writeStructureExtraFiles

// StructureRows and UnreadForms expose the contract table and the
// default-branch forms to TestTypeScriptStructureContract, which runs the
// indexer and so lives in the external test package.
var StructureRows, UnreadForms = structureRows, unreadForms

// SetStructureTagHook installs (or, with nil, removes) the hook every
// branch of the contract reports its row to.
func SetStructureTagHook(hook func(row string)) { structureTagHook = hook }

// BridgeRows exposes the bridge contract table to
// TestTypeScriptBridgesContract.
var BridgeRows = bridgeRows

// SetBridgeTagHook installs (or, with nil, removes) the hook every branch
// of the bridge contract reports its row to.
func SetBridgeTagHook(hook func(row string)) { bridgeTagHook = hook }
