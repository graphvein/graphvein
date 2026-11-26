package python

// WriteStructureExtraFiles lets the external test package write the
// fixture cases the repository cannot store (structureExtraFiles) and
// restore the fixture's inert names.
var WriteStructureExtraFiles = writeStructureExtraFiles

// ReferencePython is the contract's reference Python version: the oracle
// tests compare with that interpreter only.
const ReferencePython = referencePython

// StructureExtensionModule is the fixture's extension module file name
// (the reference build's suffix).
const StructureExtensionModule = structureExtensionModule
