package server

// workspaceRow is one row of the workspace claim contract
// (docs/contract/claims/workspace.md, server rows). The table and this
// list are kept 1:1 by TestWorkspaceContractTable, and each row names the
// test that checks it. A refusal of POST /api/v0/workspace/check or
// POST /api/v0/store/check carries its row (refusal.row).
type workspaceRow struct {
	ID    string
	Topic string
	// Test is the test function that exercises the row.
	Test string
}

// workspaceRows is the server part of the contract in table order.
var workspaceRows = []workspaceRow{
	{"WS-1", "registry file", "TestRegistryPath"},
	{"WS-2", "registry format", "TestRegistryFormat"},
	{"WS-3", "atomic writes", "TestRegistryAtomicWrite"},
	{"WS-4", "entry identity", "TestEntryID"},
	{"WS-5", "folders that are gone", "TestRegistryKeepsGoneFolders"},
	{"WS-6", "who registers", "TestServeWorkspace"},
	{"WS-7", "the list", "TestWorkspaceList"},
	{"WS-8", "entry state", "TestWorkspaceMovedFolder"},
	{"WS-9", "entry freshness", "TestWorkspaceProjectFreshnessListed"},
	{"WS-10", "project members", "TestWorkspaceListStates"},
	{"WS-11", "entry job", "TestWorkspaceAddRuns"},
	{"WS-12", "current entry and suggestion", "TestWorkspaceList"},
	{"WS-13", "per-index dispatch", "TestWorkspaceDispatch"},
	{"WS-14", "ws errors", "TestWorkspaceDispatch"},
	{"WS-15", "open errors", "TestWorkspaceDispatch"},
	{"WS-16", "open indexes", "TestWorkspaceLRU"},
	{"WS-17", "answer cache", "TestWorkspaceCache"},
	{"WS-18", "cache invalidation", "TestWorkspaceCacheRun"},
	{"WS-19", "cache bounds", "TestWorkspaceCacheBounds"},
	{"WS-20", "background checks", "TestWorkspaceRecheckNotOpen"},
	{"WS-21", "POST guards", "TestWorkspaceGuards"},
	{"WS-22", "absolute paths only", "TestWorkspaceCheckPath"},
	{"WS-23", "symbolic links", "TestWorkspaceCheckPath"},
	{"WS-24", "no such folder", "TestWorkspaceCheckPath"},
	{"WS-25", "not a folder", "TestWorkspaceCheckPath"},
	{"WS-26", "filesystem root", "TestWorkspaceCheckPath"},
	{"WS-27", "home folder", "TestWorkspaceCheckPath"},
	{"WS-28", "configuration folders", "TestWorkspaceCheckPath"},
	{"WS-29", "index and git folders", "TestWorkspaceCheckPath"},
	{"WS-30", "inside a registered folder", "TestWorkspaceCheckPath"},
	{"WS-53", "system folders", "TestWorkspaceCheckSystemFolders"},
	{"WS-54", "folders holding registered entries", "TestWorkspaceCheckContains"},
	{"WS-31", "already registered", "TestWorkspaceCheckPath"},
	{"WS-32", "project folders", "TestWorkspaceCheckPath"},
	{"WS-33", "repository folders", "TestWorkspaceCheckPath"},
	{"WS-57", "names shared in one store", "TestWorkspaceSharedStore"},
	{"WS-34", "index presence", "TestWorkspaceCheckIndexPresence"},
	{"WS-35", "confirm value", "TestWorkspaceConfirm"},
	{"WS-36", "add", "TestWorkspaceAddRunRefused"},
	{"WS-37", "add without a run", "TestWorkspaceIndexAddedEntry"},
	{"WS-38", "add a project", "TestWorkspaceAddProject"},
	{"WS-39", "remove", "TestWorkspaceRemove"},
	{"WS-40", "index run shape under --project", "TestIndexRunProjectShape"},
	{"WS-D", "any other path error", "TestWorkspaceCheckPath"},
	{"WS-70", "each entry's store", "TestServeWorkspace"},
	{"WS-71", "two indexes, foreign store folder", "TestWorkspaceCheckStore"},
	{"WS-72", "suggestion", "TestWorkspaceList"},
	{"WS-73", "store in a check", "TestWorkspaceCheckStore"},
	{"WS-74", "store place", "TestWorkspaceCheckStoreChoice"},
	{"WS-75", "typed store folders", "TestWorkspaceCheckStoreChoice"},
	{"WS-76", "add binds the store", "TestWorkspaceAddStorePlace"},
	{"WS-77", "Storage settings", "TestStoreSettings"},
	{"WS-78", "move check", "TestStoreMoveCheck"},
	{"WS-79", "move", "TestStoreMove"},
	{"WS-80", "folder for new indexes", "TestStoreDefaultDir"},
	{"WS-81", "runs after a move", "TestServeEntryAfterMove"},
}
