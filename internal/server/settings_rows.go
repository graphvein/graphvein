package server

// settingsRow is one claim row of docs/contract/claims/settings.md and the
// test that checks it.
type settingsRow struct {
	ID   string
	Test string
}

// settingsRows is the claim contract in table order (T039, ADR 0088).
// Rows ST-1..ST-11, ST-31 and ST-32 are the form's keys: config.SettingKeys holds one key
// per row, in the same order (TestSettingsContractTable). Rows
// ST-41..ST-46 are the settings an index run was built with; ST-47 is
// graphvein status --project, ST-48 and ST-49 the web UI (their tests are
// named "web/<file>: <test name>").
var settingsRows = []settingsRow{
	{"ST-1", "TestSettingValues"},
	{"ST-2", "TestSettingValues"},
	{"ST-3", "TestSettingValues"},
	{"ST-4", "TestSettingValues"},
	{"ST-5", "TestSettingValues"},
	{"ST-6", "TestSettingValues"},
	{"ST-7", "TestSettingValues"},
	{"ST-8", "TestSettingValues"},
	{"ST-9", "TestSettingValues"},
	{"ST-10", "TestSettingValues"},
	{"ST-11", "TestSettingValues"},
	{"ST-31", "TestSettingValues"},
	{"ST-32", "TestSettingValues"},
	{"ST-12", "TestSettingsGet"},
	{"ST-13", "TestSettingsGet"},
	{"ST-14", "TestSettingsGet"},
	{"ST-15", "TestSettingsInvalidConfig"},
	{"ST-16", "TestSettingsGet"},
	{"ST-17", "TestSettingsAppliesAtOnce"},
	{"ST-18", "TestSettingsPreviewApply"},
	{"ST-19", "TestSettingsGet"},
	{"ST-20", "TestSettingsProject"},
	{"ST-21", "TestSettingsProject"},
	{"ST-22", "TestSettingsEditGuards"},
	{"ST-23", "TestSettingsConcurrentEdit"},
	{"ST-24", "TestSettingsPreviewApply"},
	{"ST-25", "TestSettingsPreviewApply"},
	{"ST-26", "TestSettingsTypedValues"},
	{"ST-27", "TestSettingsEditGoldens"},
	{"ST-28", "TestSettingsRawText"},
	{"ST-29", "TestSettingsPreviewApply"},
	{"ST-30", "TestSettingsConcurrentEdit"},
	{"ST-41", "TestRunSettings"},
	{"ST-42", "TestIndexInputsCanonical"},
	{"ST-43", "TestIndexInputsChanged"},
	{"ST-44", "TestIndexInputsUnknown"},
	{"ST-45", "TestIndexInputKeys"},
	{"ST-46", "TestStatusSettings"},
	{"ST-47", "TestProjectStatusSettings"},
	{"ST-48", "web/src/shell/FreshnessBadge.test.tsx: settings changed"},
	{"ST-49", "web/src/sections/settings/model.test.ts: precedence ladder"},
}
