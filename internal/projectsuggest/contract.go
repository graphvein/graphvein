// Package projectsuggest proposes `consumes` declarations for a project
// manifest (ADR 0075, T022) from files it reads statically: env
// files, Compose files and dev-server proxy configs. It never executes
// anything and never writes: a proposal only becomes a declaration when
// the user accepts it (`graphvein project suggest --accept N`, or the
// UI), and only a declaration changes links.
//
// The readers follow a claim contract (Rows, documented in
// docs/cli/project.md "Suggestions"): each read form says what it yields
// and when it is refused, and every form no row covers goes to the single
// unread row, RowUnread.
package projectsuggest

// Row is one row of the suggestion claim contract.
type Row struct {
	ID string
	// Form is the file form the row reads, or the matching case it decides.
	Form string
}

// Read rows: what a file yields.
const (
	// RowEnvURL: an env file entry whose value is an http(s) URL is an
	// address the member calls, through the base named by its key.
	RowEnvURL = "SG-E1"
	// RowEnvPort: an env file port entry says the member listens on that
	// port of the local machine.
	RowEnvPort = "SG-E2"
	// RowComposeEnvironment: an environment entry of a Compose service
	// built from a member is an address that member calls.
	RowComposeEnvironment = "SG-C1"
	// RowComposePorts: a ports entry of a Compose service built from a
	// member: the published port is a local port, the container port a
	// port of the service name inside that Compose file.
	RowComposePorts = "SG-C2"
	// RowComposeExpose: an expose entry is a container port.
	RowComposeExpose = "SG-C3"
	// RowViteProxy: a literal Vite server.proxy entry.
	RowViteProxy = "SG-V1"
	// RowWebpackProxyObject: a literal webpack devServer.proxy object entry.
	RowWebpackProxyObject = "SG-W1"
	// RowWebpackProxyArray: a literal webpack devServer.proxy array entry.
	RowWebpackProxyArray = "SG-W2"
	// RowCRAProxy: a Create React App package.json proxy.
	RowCRAProxy = "SG-R1"
	// RowDefineEnv: a literal webpack DefinePlugin or Vite define entry
	// `NAME: JSON.stringify(process.env.X)`: calls through NAME use X, so
	// an env file address of X in the config's folder also proposes NAME.
	RowDefineEnv = "SG-D1"
	// RowDefineURL: a literal define entry whose value is a URL string is
	// an address the member calls through the constant.
	RowDefineURL = "SG-D2"
)

// Match rows: what an address read above becomes.
const (
	// RowMatchLocal: a loopback address whose port exactly one other member
	// declares; a proposal.
	RowMatchLocal = "SG-M1"
	// RowMatchService: a Compose environment address naming a service of
	// the same Compose file built from exactly one other member, with that
	// container port; a proposal.
	RowMatchService = "SG-M2"
	// RowNoMatch: no other member, or several, declare the address;
	// unmatched evidence, no proposal.
	RowNoMatch = "SG-M3"
	// RowMatchServiceName: an address outside a Compose file naming a
	// Compose service of the same member's Compose files, every service of
	// that name built from one other member, with that container port; a
	// proposal.
	RowMatchServiceName = "SG-M4"
	// RowNoCalls: a matched address whose base, define constants and
	// origin no stored call of the consumer records (or the consumer has
	// no stored index); unmatched evidence, no proposal.
	RowNoCalls = "SG-M5"
)

// RowUnread is the single default: every form no other row covers, and
// every refused form, is listed as not read and proposes nothing.
const RowUnread = "SG-U"

// Rows is the claim contract in table order; docs/cli/project.md lists the
// same rows (a test keeps them 1:1).
var Rows = []Row{
	{RowEnvURL, "env file `KEY=VALUE` with an http(s) URL value"},
	{RowEnvPort, "env file port key with an integer value"},
	{RowComposeEnvironment, "Compose `environment` URL of a service built from a member"},
	{RowComposePorts, "Compose `ports` of a service built from a member"},
	{RowComposeExpose, "Compose `expose` of a service built from a member"},
	{RowViteProxy, "Vite `server.proxy` literal entry"},
	{RowWebpackProxyObject, "webpack `devServer.proxy` literal object entry"},
	{RowWebpackProxyArray, "webpack `devServer.proxy` literal array entry"},
	{RowCRAProxy, "Create React App `package.json` `proxy`"},
	{RowDefineEnv, "webpack `DefinePlugin` or Vite `define` literal entry reading `process.env`"},
	{RowDefineURL, "webpack `DefinePlugin` or Vite `define` literal URL entry"},
	{RowMatchLocal, "loopback address, one other member declares the port"},
	{RowMatchService, "Compose service address, one other member builds the service"},
	{RowNoMatch, "address no other member, or several, declare"},
	{RowMatchServiceName, "service name outside Compose, one other member builds the service"},
	{RowNoCalls, "matched address no stored call of the consumer uses"},
	{RowUnread, "everything else"},
}
